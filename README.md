# slo-report

[![ci](https://github.com/OTA-EITA/slo-report/actions/workflows/ci.yml/badge.svg)](https://github.com/OTA-EITA/slo-report/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/OTA-EITA/slo-report/badge)](https://securityscorecards.dev/viewer/?uri=github.com/OTA-EITA/slo-report)

SLO を YAML で定義すると、次の 2 つを行う CLI です。

1. **`generate`**: Prometheus / VictoriaMetrics（vmalert）用に、エラー率の記録ルールと**複数ウィンドウ・複数バーンレートのアラート**を生成する
2. **`report`**: Prometheus 互換 API に問い合わせ、**エラーバジェットの残量・バーンレート・枯渇までの時間**を SLO レビュー用のレポートにまとめる

Define SLOs once in YAML, generate multi-window multi-burn-rate alerts for Prometheus or vmalert,
and produce error budget reports for SLO reviews.

## なぜ作ったか

SLO を運用し始めると、次の 2 つが手作業になりがちです。

- バーンレートアラートの閾値計算（SLO の期間や目標値が変わるたびに、14.4 倍・6 倍…を計算し直す）
- 週次の SLO レビューで、各サービスのバジェット残量を Grafana から拾って表にまとめる作業

定義を 1 か所にまとめ、アラートとレポートの両方をそこから作れるようにしました。

## 使い方

### 1. SLO を定義する

```yaml
slos:
  - name: api-availability
    service: api
    objective: 99.9        # 良いリクエストの割合（%）
    window: 30d            # SLO の期間
    runbookURL: https://example.com/runbooks/api-availability
    sli:                   # エラー率 = errors / total。${window} が各ウィンドウに置き換わる
      errors: sum(rate(http_requests_total{job="api",code=~"5.."}[${window}]))
      total: sum(rate(http_requests_total{job="api"}[${window}]))
```

```sh
slo-report validate slos.yaml
```

未知のフィールド（`objectve` などの打ち間違い）、範囲外の目標値、`${window}` の書き忘れ、名前の重複はすべてエラーになります。

### 2. アラートルールを生成する

```sh
slo-report generate slos.yaml > rules.yaml
promtool check rules rules.yaml
```

[Google SRE Workbook の「Alerting on SLOs」](https://sre.google/workbook/alerting-on-slos/)で推奨されている 4 つの条件を、SLO の期間に合わせて計算します。

| 重大度 | 長いウィンドウ | 短いウィンドウ | 意味 | 30d SLO での閾値 |
|---|---|---|---|---|
| page | 1h | 5m | 1 時間でバジェットの 2% を消費 | 14.4 倍 |
| page | 6h | 30m | 6 時間でバジェットの 5% を消費 | 6 倍 |
| ticket | 1d | 2h | 1 日でバジェットの 10% を消費 | 3 倍 |
| ticket | 3d | 6h | 3 日でバジェットの 10% を消費 | 1 倍 |

SLO の期間が 7d なら 1h の閾値は 3.36 倍、のように自動で変わります。長いウィンドウが SLO の期間以上になる条件は生成しません。

生成例（[`examples/rules.yaml`](examples/rules.yaml) より）:

```yaml
      - alert: SLOErrorBudgetBurn
        expr: |-
          slo:error_ratio:rate1h{slo="api-availability"} > 0.0144
          and
          slo:error_ratio:rate5m{slo="api-availability"} > 0.0144
        for: 2m
        labels:
          long_window: 1h
          service: api
          severity: page
          short_window: 5m
          slo: api-availability
        annotations:
          description: Error ratio of api-availability is above 0.0144 over both 1h and 5m. At this rate 2% of the 30d error budget is spent within 1h.
          runbook_url: https://example.com/runbooks/api-availability
          summary: api-availability is burning its error budget 14.4x too fast
```

### 3. エラーバジェットのレポートを出す

```sh
slo-report report --prometheus http://prometheus:9090 --output markdown slos.yaml
```

出力例（CI の E2E で、VictoriaMetrics に合成データを入れて実行した結果）:

| SLO | SERVICE | OBJECTIVE | WINDOW | SLI | BUDGET LEFT | BURN 1H | BURN 6H | BURN 1D | EXHAUSTED IN | VERDICT |
|---|---|---|---|---|---|---|---|---|---|---|
| checkout-availability | checkout | 99.9% | 30d | 98.000% | -1900.0% | 20.00x | 20.00x | 20.00x | - | 🔴 EXHAUSTED |
| search-availability | search | 99.9% | 30d | 99.990% | 90.0% | 0.10x | 0.10x | 0.10x | 270.0d | 🟢 HEALTHY |
| billing-availability | billing | 99.9% | 30d | - | - | - | - | - | - | ⚪ NO DATA |

- レポートは記録ルールに依存せず、SLI のクエリを直接評価します。ルールを入れる前からレポートを出せます。
- 「EXHAUSTED IN」は 6h のバーンレートが続いた場合にバジェットが尽きるまでの時間です。
- バジェットを使い切った SLO があると終了コード 1 になります（`--fail-on at-risk` / `never` で変更可能）。CI やリリース判定のゲートに使えます。

| フラグ | 既定値 | 説明 |
|---|---|---|
| `--prometheus` | `$PROMETHEUS_URL` | Prometheus 互換 API の URL（Prometheus、VictoriaMetrics、Thanos など） |
| `--bearer-token-file` | - | 認証が必要な API 用のトークンファイル |
| `--output` | `table` | `table` / `json` / `markdown` |
| `--fail-on` | `exhausted` | `exhausted` / `at-risk` / `never` |

## インストール

[Releases](https://github.com/OTA-EITA/slo-report/releases) からバイナリをダウンロードするか、Go でインストールします。

```sh
go install github.com/OTA-EITA/slo-report/cmd/slo-report@latest
```

## テスト

| 種類 | 内容 |
|---|---|
| ユニットテスト | 閾値の計算（Workbook の 14.4 / 6 / 3 / 1 を再現）、バリデーション、生成結果のゴールデンファイル比較、Prometheus API クライアント |
| promtool | 生成したルールを `promtool check rules` で検証し、[`examples/rules_test.yaml`](examples/rules_test.yaml) で「エラー率 10% の障害では page アラートが発火し、0.05% の正常時はどのアラートも発火しない」ことを Prometheus のルールエンジンで確認 |
| E2E | VictoriaMetrics に 6 時間分の合成トラフィックを取り込み、レポートの判定・バーンレート・終了コードを確認（[`e2e/run.sh`](e2e/run.sh)） |

```sh
make test
make lint
```

## License

MIT
