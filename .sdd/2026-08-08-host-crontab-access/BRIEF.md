# BRIEF · host crontab access

## 要解決什麼

讓服務直接跑在 host 上、看**使用者真正在用的那份 crontab**，而不是容器內自管的一份副本。

## 為什麼需要新的存取方式

macOS（以及多數 Linux 發行版）的 crontab spool 目錄權限是 `drwx------ root:wheel`：

```
$ ls -ld /var/at/tabs
drwx------  3 root  wheel  96 /var/at/tabs
$ cat /var/at/tabs/james
cat: /var/at/tabs/james: Permission denied
$ crontab -l
5 * * * * ...        ← 讀得到
```

`crontab` 是 setuid 執行檔，所以**唯一不需要 root 就能存取使用者 crontab 的方法是走
`crontab` 命令本身**。既有的 `CrontabDocumentRepository`（直接 `os.ReadFile`）在 host
模式下必然 permission denied。

替代方案是叫使用者 `sudo` 跑這個服務 —— 一個能執行任意指令的 web 服務用 root 跑，
不值得為了省一個 repository 實作而接受。

## 共識

- 新增第二個 `ICrontabDocumentRepository` 實作：讀用 `crontab -l`，寫用 `crontab <file>`。
  領域層與所有 service 完全不知道差別 —— 這正是當初把它做成介面的理由。
- 以 `CRONTAB_SOURCE` 環境變數選擇（`file` 預設／`crontabCommand`）。
- 版本指紋改用**內容雜湊**：命令模式下我們 stat 不到那個檔案，拿不到 mtime。
- 寫入前一律備份「當下 `crontab -l` 的輸出」。`crontab <file>` 會**整份取代**使用者的
  crontab，這是全專案風險最高的一個操作。
- 一個 `make start-host` target 把所有路徑指到 `$HOME/.local/state/crontab-watcher/`，
  並用 `make build` 產生的執行檔（不是 `go run` 的暫存檔，那寫進 crontab 會失效）。

## 不做什麼

- 不做 `sudo`／不以 root 執行
- 不管別的使用者的 crontab（`crontab -u` 需要 root）
- 不做 `/etc/crontab` 或 `/etc/cron.d`
