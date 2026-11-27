# Fleet 里程碑批次派工

`/ship #<issue>` 指向的 issue 若屬於某個 GitHub milestone，而且該 milestone 還有其他未結案的 issue，Fleet 可以接手整個 milestone。

## 授權

| 指令 | 行為 |
|------|------|
| `/project milestone ask`（預設） | 詢問一次：接手整個里程碑，或只派這一張 |
| `/project milestone auto` | 擁有者送出 `/ship #<issue>` 就直接接手 |

只有擁有者可以回覆詢問或改這個設定。批次建立時帶的 `--model`、`--budget` 套用到批次派出的每個任務。

## 派工規則

- 相依關係讀 issue 內文中以 `依賴`、`相依`、`Depends on`、`Blocked by` 開頭的行。相依的 issue **以 completed 結案** 才算完成；任務做完但 issue 還開著不算。
- 同時排入的任務數不超過專案的 `max_concurrent`。等你審核或決定的任務不佔名額。
- 已經有任務的 issue 沿用該任務。批次派出的任務被取消後不會自動重派，要重做請再送一次 `/ship #<issue>`。
- Epic（`fleet.epic_labels`）與排除標籤的 issue 不派工；受阻標籤的 issue 等標籤移除。
- 目標分支在批次建立時固定。專案目標分支之後改變，批次停止派工並在摘要中說明。

## 重整

批次在服務啟動、任務狀態改變及固定間隔時重讀 GitHub 與任務紀錄，所以手動合併或結案後會自動跟上。讀取 GitHub 失敗時維持原狀，不派工。再送一次 `/ship #<issue>` 會顯示同一批次的進度。

## 設定

`config.json` 的 `fleet.milestone`（範例見 `config.example.json`）：`dependency_markers`、`blocked_labels`、`exclude_labels`、`reconcile_seconds`。

## 尚未包含

自動合併與結案 (#241)、外部阻礙回報與接續 (#242)、完工判定與 tag (#243)。目前也沒有 dashboard 頁面。
