# BRIEF · manual trigger

## 要解決什麼

兩件事，共用同一條執行路徑：

1. **wrapper**（`cronwatch run`）——讓由 cron 觸發的 managed job 留下完整紀錄（exit code、耗時、輸出）。這是「執行結果可信」的唯一來源。
2. **手動觸發**（`POST /jobs/:jobId/run`）——從瀏覽器立刻跑一次，不必等排程。

## 共識

- 兩者共用 `JobExecutionService`，差別只在 `TriggerSource` 與是否套逾時（手動套、排程不套，見 D-10）。
- **先寫 `running` 紀錄再執行**，這樣程序被砍時留得下痕跡；server 啟動時把殘留的 `running` 掃成 `unknown` 並標原因。
- 同一 job 不允許並發（見 D-09）→ 409。
- wrapper **以子程序的 exit code 作為自己的 exit code**，讓 cron 的錯誤語意不失真；wrapper 自身的錯誤只寫 stderr，不影響 exit code。
- 逾時以 process group kill 收掉子孫程序（單純 kill 主程序會留下孤兒）。

## 不做什麼

- 不做「取消正在執行的 run」（個人用，需要時直接 `kill`）
- 不做重試、不做失敗通知（未來切片）
- 不對 foreign job 提供手動觸發？**提供**——但會明確告知「這次執行會被記錄，但排程觸發的仍然不會」
