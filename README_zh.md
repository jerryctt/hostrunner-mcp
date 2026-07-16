[English](README.md) | 繁體中文

# hostrunner-mcp

[![CI](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

本機 MCP 伺服器 + Claude plugin,在你的機器上執行唯讀的 Codex 程式碼審查 —— 把 Claude 沙箱橋接到 Host 的 codex CLI,達成邊改邊審的迴圈。

---

## 目的與動機

[Claude Cowork](https://claude.ai) 在隔離的沙箱中編輯程式碼。該沙箱無法執行 Host 機器上的 `codex` CLI（沙箱中未安裝 `codex`，也無法存取您的授權憑證或本地檔案）。**hostrunner-mcp** 解決了這個問題：

- 它以**原生行程**在您的 Mac 或 Linux 機器上執行，由 Claude Desktop 透過 stdio 啟動。
- 它完整存取 Host 檔案系統、`codex` 執行檔以及您的授權憑證。
- Cowork 編輯您分享到 Session 的資料夾中的檔案，再呼叫伺服器的工具，觸發 Host 端的唯讀 `codex review` 審查，並將結果傳回 Cowork。
- 這讓您無需離開 Cowork 即可完成緊湊的**編輯 → 審查 → 編輯**迴圈。

---

## 運作原理

```
┌─────────────────────────────────────────────────────────┐
│  Claude Cowork（沙箱）                                  │
│                                                         │
│  ┌────────────┐   編輯檔案   ┌──────────────────┐      │
│  │  AI 代理   │ ────────────►│   掛載資料夾     │      │
│  │  (Cowork)  │              │  /sessions/…/mnt │      │
│  └─────┬──────┘              └────────┬─────────┘      │
│        │  MCP 工具呼叫                │                 │
│        │  codex_review_start(folder=…) │               │
└────────┼─────────────────────────────┼─────────────────┘
         │ stdio（MCP 協定）            │ Host FS 掛載
         ▼                             ▼
┌─────────────────────────────────────────────────────────┐
│  HOST 機器                                              │
│                                                         │
│  ┌──────────────────┐  codex review  ┌──────────────┐  │
│  │  hostrunner-mcp  │ ──────────────►│  git 儲存庫  │  │
│  │  （原生行程）    │                │ /Users/…/proj│  │
│  │                  │                │  codex CLI   │  │
│  └──────────────────┘                └──────────────┘  │
│        由 Claude Desktop 啟動                           │
└─────────────────────────────────────────────────────────┘
```

---

## 功能特色

- **`codex_review_start` / `codex_review_status`** — 對未提交的變更、base 分支差異或特定 commit，以 Host 上的**背景工作（background job）**執行唯讀的 `codex review`。`codex` 自行計算差異，本端不需處理 git。採用 job 模式是因為 Claude Desktop 會在約 180 秒後強制中斷單次 MCP 工具呼叫，而實際審查常常超過這個時間；啟動與輪詢都是快速呼叫，審查本身則可跑滿您設定的 `timeout`。
- **`run_command`** — 在 Host 資料夾中以 argv 陣列執行任何白名單 CLI（例如 `git`、`codex`），從不使用 shell。
- **嚴格安全性** — 指令名稱白名單、根目錄範圍限制、符號連結解析路徑檢查、絕不使用 shell。
- **高度可設定** — 允許的根目錄、允許的指令、逾時時間、輸出大小上限，以及選用的 codex 額外旗標。
- **稽核日誌** — 每次工具呼叫均透過 zerolog 記錄至 stderr。
- **可擴充** — 只需在設定檔中加入白名單，即可新增其他 CLI（例如 `gemini`）。

---

## 系統需求

| 需求項目 | 說明 |
|---|---|
| **codex CLI** | 必須在 Host 上安裝並完成授權（`codex` 在 `$PATH` 中） |
| **Claude Desktop** | 用於註冊並啟動 MCP 伺服器 |
| **Go 1.25+** | 僅在從原始碼建構時需要；下載預建執行檔則無需安裝 Go |

---

## 安裝方式

### 方式一：使用 `go install`（若已安裝 Go，建議此方式）

```bash
go install github.com/jerryctt/hostrunner-mcp/cmd/hostrunner@latest
```

執行檔將放置於 `$GOPATH/bin/hostrunner`（通常為 `~/go/bin/hostrunner`）。

### 方式二：下載發行版執行檔

從 [Releases 頁面](https://github.com/jerryctt/hostrunner-mcp/releases)下載適合您平台的預建執行檔，解壓後將 `hostrunner` 放入 `$PATH` 中（例如 `/usr/local/bin/hostrunner`）。

### 方式三：從原始碼建構

```bash
git clone https://github.com/jerryctt/hostrunner-mcp.git
cd hostrunner-mcp
make build          # 產出 ./hostrunner
```

### 方式四：以 Claude 外掛程式安裝（Marketplace）

**在 Claude Desktop 中：** 前往「新增 Marketplace」→ 輸入 `jerryctt/hostrunner-mcp`，再安裝 **hostrunner** 外掛程式。

**在 Claude Code 中：**
```
/plugin marketplace add jerryctt/hostrunner-mcp
```
從清單中安裝 `hostrunner` 外掛程式。

透過 Marketplace 安裝，可一次完成 MCP 伺服器（由隨附啟動程式自動啟動）與 `codex-loop` 技能的安裝。

**前置條件 — 建立設定檔：** 伺服器預設從 `~/.config/hostrunner/config.yaml` 讀取設定。請在啟動 Claude Desktop/Code 前依照 `examples/config.example.yaml` 建立設定檔：

```bash
mkdir -p ~/.config/hostrunner
cp examples/config.example.yaml ~/.config/hostrunner/config.yaml
# 編輯檔案，填入您自己的路徑
```

您也可以透過 `-config` 旗標或 `HOSTRUNNER_CONFIG` 環境變數來覆寫設定檔路徑。

> **macOS GUI 應用程式與環境變數：** macOS 不會將 Shell 的環境變數傳遞給 GUI 應用程式（如 Claude Desktop），因此在 `.zshrc` 或 `.bashrc` 中設定的 `HOSTRUNNER_CONFIG` 對從 Claude Desktop 啟動的外掛程式無效。請將設定檔放在預設路徑 `~/.config/hostrunner/config.yaml`，伺服器無需任何環境變數即可找到它。

**macOS/Linux：** 隨附的 `scripts/launch.sh` 啟動程式會在首次執行時下載符合目前作業系統與架構的發行版執行檔，並在執行前對照發行版的 `checksums.txt` 進行校驗碼驗證。無需手動安裝執行檔。

**Windows：** 啟動程式不支援 Windows。請手動安裝執行檔（方式二），並依「向 Claude Desktop 註冊」章節所述，透過 `claude_desktop_config.json` 進行設定。

> **信任說明：** 啟動程式會從 GitHub Releases 下載並執行執行檔，並以發行版的校驗碼進行驗證。若有疑慮，請檢閱 `scripts/launch.sh` 的原始碼。

---

## 設定

伺服器依以下順序解析設定檔路徑：
1. 若提供了 `-config` 旗標，優先使用其指定的路徑。
2. 若設定了 `HOSTRUNNER_CONFIG` 環境變數，使用其值。
3. 預設路徑 `~/.config/hostrunner/config.yaml`。

**建議：** 將設定檔放在 `~/.config/hostrunner/config.yaml`。在 macOS 上以外掛程式搭配 Claude Desktop 使用時，這點尤其重要——macOS GUI 應用程式不會繼承 Shell 的環境變數，因此在 Shell 設定檔中設定的 `HOSTRUNNER_CONFIG` 對從 Claude Desktop 啟動的伺服器無效。

請從內附範本建立設定檔，**範本中的路徑為佔位符，請替換成您自己的路徑**：

```bash
mkdir -p ~/.config/hostrunner
cp examples/config.example.yaml ~/.config/hostrunner/config.yaml
```

```yaml
# 工具允許存取的絕對 Host 路徑。
# 超出這些根目錄的請求將被拒絕。
allowed_roots:
  - /Users/yourname/code

# run_command 可呼叫的 CLI 執行檔名稱。
# codex_review_start 只需要 'codex'——本端不處理 git。
# 如需在 run_command 中使用其他工具（如 git、gemini），請在此新增。
allowed_commands:
  - codex

# 單一 Host 指令的最大執行時間（codex review 本身，或一次 run_command）。
# 必須帶單位（例如 600s、10m），純數字會被拒絕。審查以背景 job 執行，
# 因此這個值可以放心設得比 MCP client 約 180 秒的單次呼叫上限更大。
timeout: 600s

# 單次工具呼叫回傳的最大位元組數（超出時輸出將被截斷並附上提示）。
max_output_bytes: 200000

# 附加到每次 'codex review' 呼叫的選用額外旗標（例如模型覆寫設定）。
# codex_extra_args: ["-c", "model=o3"]

# 是否即時將 codex/指令輸出轉送至伺服器的 stderr（預設：true）。
# Claude Desktop 會將此記錄到 ~/Library/Logs/Claude/mcp-server-hostrunner.log。
# 啟用 stream_output（預設）後，codex 的即時輸出可在該日誌中查看。
# 設為 false 可關閉此功能。
stream_output: true
```

> **注意：** 設定檔只在**伺服器啟動時讀取一次**。Claude Desktop 啟動時才會產生伺服器行程，因此修改 `config.yaml` 後必須完全重啟 Claude Desktop 才會生效。啟動時實際生效的設定值會記錄到 `~/Library/Logs/Claude/mcp-server-hostrunner.log`（尋找 `config loaded` 那一行，會顯示設定檔路徑與實際生效的 timeout）。

---

## 向 Claude Desktop 註冊

將以下設定加入 Claude Desktop 的設定檔（macOS 上通常位於 `~/Library/Application Support/Claude/claude_desktop_config.json`）：

```json
{
  "mcpServers": {
    "hostrunner": {
      "command": "/usr/local/bin/hostrunner",
      "args": ["-config", "/Users/yourname/.config/hostrunner/config.yaml"]
    }
  }
}
```

請將 `/usr/local/bin/hostrunner` 替換為實際的執行檔路徑（例如 `~/go/bin/hostrunner`），並相應更新設定檔路徑。儲存庫中的 `examples/claude_desktop_config.example.json` 也提供了相同的範例。

編輯完設定後，請重新啟動 Claude Desktop。您應可在 MCP 伺服器清單中看到 **hostrunner**。

---

## 可用工具

### `codex_review_start`

對 Host git 儲存庫中的變更啟動唯讀的 `codex review`，以 Host 上的**背景工作**執行。此呼叫會立即回傳 `job_id`；請用 `codex_review_status` 輪詢結果。使用原生的 `codex review` 子指令，由 codex 自行計算差異，不修改任何檔案。只需 `codex` CLI 即可，本端不處理 git。

| 參數 | 類型 | 必填 | 說明 |
|---|---|---|---|
| `folder` | string | 是 | **HOST** 絕對路徑，指向 git 儲存庫（例如 `/Users/you/proj`）。不能是 `/sessions/…` 沙箱路徑。 |
| `scope` | string | 否 | `uncommitted`（預設，涵蓋已暫存、未暫存及未追蹤的變更）、`base`（與 base 分支比較）或 `commit`（特定 commit） |
| `base` | string | 否 | 基礎分支或 ref；當 `scope=base` 時必填（例如 `main`） |
| `commit` | string | 否 | Commit SHA；當 `scope=commit` 時必填 |
| `prompt` | string | 否 | 自訂審查指示（例如 `"聚焦於錯誤處理"`）。只在使用者明確要求聚焦審查時傳入；一般審查請省略。`prompt` 可以和任何 `scope` 併用：codex CLI 不允許 scope 旗標與位置參數 `[PROMPT]` 同時出現，伺服器會自動把 scope 併入 prompt 文字。 |

**回傳：** `job_id`、模式與資料夾。

```
Review started in the background.
job_id: 3fa4c19e02d1
mode: uncommitted
folder: /Users/you/proj
```

### `codex_review_status`

取得背景審查的狀態／結果。每次呼叫最多阻塞約 50 秒等待完成（long-poll），然後回傳完成的審查結果或 `running` 提示——請用同一個 `job_id` 反覆呼叫直到完成。完成的結果會保留約 30 分鐘。

| 參數 | 類型 | 必填 | 說明 |
|---|---|---|---|
| `job_id` | string | 是 | `codex_review_start` 回傳的 id |

**回傳（已完成）：**

```
status: completed (job 3fa4c19e02d1, elapsed 4m12s)

Mode: uncommitted (codex exit 0)

--- Codex review ---
… codex 審查結果 …
```

**回傳（執行中）：**

```
status: running (job 3fa4c19e02d1, elapsed 1m40s)
The review is still in progress. Call codex_review_status again with the same job_id.
```

### `run_command`

在 Host 資料夾中執行白名單中的指令（argv 陣列，不使用 shell）。

| 參數 | 類型 | 必填 | 說明 |
|---|---|---|---|
| `command` | string | 是 | 執行檔名稱；必須在 `allowed_commands` 中 |
| `args` | string[] | 否 | 以字串陣列表示的參數 |
| `folder` | string | 是 | 位於 `allowed_root` 內的 **HOST** 絕對路徑 |

**回傳：** 結束碼、執行耗時、stdout 和 stderr。

```
exit=0 elapsed=1.23s
--- stdout ---
…
--- stderr ---
…
```

---

## 安裝 Cowork 技能

儲存庫在 `skills/codex-loop/SKILL.md` 提供了一個現成的 Cowork 技能。此技能教導 Cowork 在每輪編輯後自動執行 `codex_review_start` / `codex_review_status`。

將技能目錄複製到您的 Cowork 技能資料夾以完成安裝：

```bash
cp -r skills/codex-loop ~/.config/claude/skills/
```

（技能目錄的確切位置可能因您的 Cowork 安裝而有所不同，請參閱 Cowork 設定中的說明。）

安裝完成後，您可以在 Cowork 中透過以下方式觸發此技能：

- 「用 codex 審查我的變更」
- 「對我的編輯執行 codex review」
- 「開始編輯與審查的迴圈」

---

## 使用方式

以下是在 Claude Cowork 中進行**編輯 → 審查 → 編輯**的典型工作流程：

1. **與 Cowork Session 分享您的專案資料夾。** Cowork 會將其掛載於 `/sessions/<id>/mnt/yourproject`。

2. **使用 Cowork 的 Read/Write/Edit 工具編輯檔案。** 例如，Cowork 編輯了 `/sessions/<id>/mnt/yourproject/src/handler.go`。

3. **啟動 codex 審查。** 以 **Host** 路徑呼叫 `codex_review_start` 工具：
   ```
   codex_review_start(
     folder = "/Users/yourname/code/yourproject"
   )
   ```
   請傳入 Host 路徑 — 即您在 Finder 或終端機中看到的路徑 — **而非** `/sessions/…` 沙箱路徑。工具會立即回傳 `job_id`，審查在背景執行。

4. **輪詢審查結果。** 以 `job_id` 呼叫 `codex_review_status`，直到回傳 `status: completed` 與 codex 的審查意見。每次輪詢最多阻塞約 50 秒，因此數分鐘的審查只需幾次呼叫，永遠不會撞到 MCP client 約 180 秒的單次呼叫上限。

5. **反覆迭代。** 修正 Cowork 找到的問題，再啟動新的審查。重複此步驟直到審查通過。

---

## 安全性

- **指令白名單。** 只有在 `allowed_commands` 中明確列出的指令才能被呼叫。任何其他指令的請求都會被拒絕。
- **根目錄範圍限制。** 每個路徑參數都會被解析（包含符號連結），並與 `allowed_roots` 進行比對。超出這些根目錄的請求將被拒絕。
- **絕不使用 Shell。** 指令以 argv 陣列透過 `os/exec` 執行，不會使用 `sh -c`，不進行 glob 展開，也不存在 shell 注入風險。
- **原生 Host 行程，而非 Docker。** 伺服器以普通 OS 行程運行，而非在容器中。在 Docker 中運行將無法存取 Host 的 `codex` 執行檔、授權憑證和檔案，完全違背了其設計目的。
- **僅限 Host 路徑。** 工具只接受絕對 Host 路徑（例如 `/Users/…`）。任何以 `/sessions/` 開頭的路徑都會被拒絕，並附上描述性錯誤訊息，指引您使用正確的 Host 路徑。
- **唯讀 codex。** `codex_review_start` 使用 `codex review` 子指令，該指令為非互動式且唯讀——由 codex 自行計算差異，不修改任何檔案。從不傳遞寫入或自動套用的旗標。選用的 `codex_extra_args` 設定欄位可傳遞額外旗標（例如模型覆寫），不影響安全性。

---

## 開發

```bash
# 建構執行檔
make build

# 執行所有測試
make test

# 程式碼靜態分析
make vet
```

---

## 授權條款

MIT — 詳見 [LICENSE](LICENSE)。
