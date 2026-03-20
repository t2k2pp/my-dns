# my-dns — 家庭内ネット健全化 DNS サーバー

> 「起動しっぱなしのパソコン」に 1 つの役割を与えるだけで、
> 家のすべての端末から YouTube などを簡単にブロックできます。

---

## このツールでできること

学校から支給されたタブレットを自宅で使うとき、子供が勝手に YouTube や TikTok を見てしまう問題を解決します。

- **YouTube・TikTok など特定サイトをブロック** — ブロックリスト（テキストファイル）に書くだけ
- **一時解除も簡単** — テキストを 1 行コメントアウトするだけ
- **子供が DNS を変えて回避できない** — ルーターで強制的にこのサーバーを使わせる
- **NextDNS / AdGuard DNS に対応** — 無料でさらに強力なフィルタリングを追加可能
- **自動学習** — 上流 DNS がブロックしたドメインを自動でリストに追加

---

## 仕組み

```
子供のタブレット
    ↓ 「youtube.com ってどこ？」
  ルーター（DHCP で DNS をこの PC に向ける）
    ↓
  [この PC で動く my-dns]
    ├─ blocklist.txt に載っている → 即ブロック（NXDOMAIN）
    └─ 載っていない → NextDNS / AdGuard に転送
                            ↓
                      フィルタリング済みの回答を返す
```

この PC さえ起動していれば、家のすべての端末に効果があります。

---

## 必要なもの

| 項目 | 内容 |
|------|------|
| 常時起動 PC | Mac または Windows（Mac mini・古い PC など何でも可）|
| ネット環境 | 自宅の Wi-Fi ルーターに管理画面でアクセスできること |
| 所要時間 | 約 30〜60 分（初回のみ）|
| 費用 | **無料**（NextDNS を使う場合は月 30 万クエリまで無料）|

---

## インストール手順（Mac 編）

### ステップ 1 — ファイルを入手する

ターミナルを開いて以下を実行します。

> **ターミナルの開き方**: Finder → アプリケーション → ユーティリティ → ターミナル

```bash
cd ~/Documents
git clone https://github.com/t2k2pp/my-dns.git
cd my-dns
```

### ステップ 2 — Go（プログラム実行エンジン）をインストール

```bash
# Homebrew が入っていない場合はまずこちら
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Go をインストール
brew install go
```

インストール後、ターミナルを一度閉じて開き直してください。

### ステップ 3 — プログラムをビルド（コンパイル）する

```bash
cd ~/Documents/my-dns
export PATH="/opt/homebrew/bin:$PATH"
make build
```

`bin/` フォルダに `dns-server` などのファイルが作られれば成功です。

### ステップ 4 — DNS プロバイダーを選ぶ

**NextDNS（推奨）**: より細かい設定ができる無料サービス

1. [nextdns.io](https://nextdns.io) でアカウント作成（無料）
2. 「新しい設定を作成」→ 6 文字の Configuration ID が発行される（例: `ab12cd`）
3. 「Setup」タブ → 「Endpoints」に専用 IP が表示される（例: `45.90.28.123`）
4. `config.nextdns.yaml` をテキストエディタで開き、`upstream_dns` を書き換える

```yaml
upstream_dns: "45.90.28.123:53"   # ← 自分のIPに変更
```

5. 保存したら次のコマンドで反映

```bash
make switch-nextdns
```

**AdGuard DNS（登録不要で今すぐ使える）**: アカウント登録なしで使える

```bash
make switch-adguard
```

### ステップ 5 — 動作テスト

ポート 53 を使うには管理者権限が必要です。

```bash
sudo ./bin/dns-server -config config.yaml
```

パスワードを聞かれたら Mac のログインパスワードを入力します。
別のターミナルで以下を試して確認します。

```bash
# youtube.com がブロックされるか確認
dig @127.0.0.1 youtube.com

# 「status: NXDOMAIN」が表示されればブロック成功！
```

`Ctrl+C` でサーバーを止めます。

### ステップ 6 — Mac 起動時に自動スタートする設定

毎回手動で起動しなくて済むよう、macOS のサービスとして登録します。

```bash
# サービスファイルをシステムに登録
sudo cp com.mydns.server.plist /Library/LaunchDaemons/com.mydns.server.plist

# サービスを開始（以降は Mac 起動のたびに自動スタート）
sudo launchctl load /Library/LaunchDaemons/com.mydns.server.plist
```

動いているか確認します。

```bash
make status
```

---

## インストール手順（Windows 編）

### ステップ 1 — Go をインストール

1. [go.dev/dl](https://go.dev/dl/) を開く
2. `go1.xx.x.windows-amd64.msi` をダウンロード
3. ダウンロードしたファイルをダブルクリックして「次へ」を押し続けてインストール
4. インストール後、**スタートメニューを閉じて開き直す**（重要）

### ステップ 2 — ファイルを入手する

スタートメニューで「PowerShell」を検索し、**右クリック → 「管理者として実行」** で開きます。

```powershell
cd $env:USERPROFILE\Documents
git clone https://github.com/t2k2pp/my-dns.git
cd my-dns
```

> **git が入っていない場合**: [git-scm.com](https://git-scm.com) からインストールするか、GitHub のページで「Code → Download ZIP」でダウンロードして展開してください。

### ステップ 3 — プログラムをビルドする

```powershell
go build -o bin\dns-server.exe .\cmd\dns-server
go build -o bin\bl-manager.exe .\cmd\bl-manager
go build -o bin\log-analyzer.exe .\cmd\log-analyzer
```

`bin\dns-server.exe` が作られれば成功です。

### ステップ 4 — DNS プロバイダーを選ぶ

Mac 編のステップ 4 と同様に設定します。
Windows ではファイルのコピーを手動で行います。

**NextDNS の場合**:

```powershell
copy config.nextdns.yaml config.yaml
```

`config.nextdns.yaml` を開いて自分の NextDNS の IP に書き換えてください。

**AdGuard の場合**:

```powershell
copy config.adguard.yaml config.yaml
```

### ステップ 5 — 動作テスト

**管理者として開いた PowerShell** で実行します。

```powershell
.\bin\dns-server.exe -config config.yaml
```

別の PowerShell ウィンドウで確認します。

```powershell
nslookup youtube.com 127.0.0.1
# 「Non-existent domain」が出ればブロック成功！
```

`Ctrl+C` でサーバーを止めます。

### ステップ 6 — Windows 起動時に自動スタートする設定

タスクスケジューラを使って自動起動を設定します。

1. スタートメニューで「タスクスケジューラ」を検索して開く
2. 右側の「タスクの作成」をクリック
3. **全般タブ**
   - 名前: `my-dns`
   - 「最上位の特権で実行する」にチェック ✓
   - 「ユーザーがログオンしているかどうかにかかわらず実行する」を選択
4. **トリガータブ** → 「新規」→「スタートアップ時」を選択
5. **操作タブ** → 「新規」
   - プログラム: `C:\Users\あなたのユーザー名\Documents\my-dns\bin\dns-server.exe`
   - 引数: `-config C:\Users\あなたのユーザー名\Documents\my-dns\config.yaml`
   - 開始: `C:\Users\あなたのユーザー名\Documents\my-dns`
6. 「OK」→ 管理者パスワードを入力

再起動して動いているか確認します。

```powershell
nslookup youtube.com 127.0.0.1
```

---

## ルーターの設定（最重要）

この PC を DNS サーバーとして家全体に適用するにはルーターの設定が必要です。

> **この PC の IP アドレスを確認する**
> - Mac: ターミナルで `ifconfig | grep "inet " | grep -v 127` を実行
> - Windows: PowerShell で `ipconfig` → 「IPv4 アドレス」を確認
> - 例: `192.168.1.37`

### ルーター管理画面を開く

ブラウザで以下のどれかにアクセスします。

- `http://192.168.1.1`
- `http://192.168.0.1`
- `http://192.168.2.1`

わからない場合は、ルーターの裏面や説明書を確認してください。

### DNS サーバーを変更する

「DHCP 設定」または「LAN 設定」のページを探し、DNS サーバーの欄を書き換えます。

```
プライマリ DNS:   192.168.1.37   ← この PC の IP
セカンダリ DNS:   94.140.14.15   ← AdGuard（この PC が落ちたときの保険）
```

> **セカンダリ DNS について**
> この PC が再起動中などでも子供のタブレットがネットにつながるようにするための保険です。
> セカンダリにも同じサービスの IP を入れることで、最低限の安全性を維持できます。

設定を保存したら、子供のタブレットの Wi-Fi を一度 OFF → ON にして反映させます。

---

## 日常の使い方

### YouTube を今すぐブロック・解除する

`blocklist.txt` をテキストエディタで開きます。

**ブロックする（`#` を取り除く）:**

```
youtube.com       ← この行を残す（ブロック中）
youtu.be
googlevideo.com
```

**一時解除する（行頭に `#` を付ける）:**

```
#youtube.com      ← コメントアウト（解除中）
#youtu.be
#googlevideo.com
```

変更を保存したら、60 秒以内に自動で反映されます。すぐ反映したい場合:

- Mac: `make reload-blocklist`
- Windows: `Invoke-WebRequest -Method POST http://127.0.0.1:8080/reload`

### よく使うブロックリストの例

```
# YouTube
youtube.com
youtu.be
googlevideo.com
ytimg.com

# TikTok
tiktok.com
tiktokv.com

# Instagram
instagram.com
cdninstagram.com

# Twitter/X
twitter.com
x.com

# Roblox
roblox.com
```

### サーバーの状態を確認する（Mac）

```bash
make status
```

表示される項目の見方:

| 項目 | 意味 |
|------|------|
| `total` | 処理した DNS クエリの総数 |
| `local_block` | このサーバーでブロックした回数 |
| `auto_learned` | 上流 DNS がブロックし自動学習した回数 |
| `forwarded` | 普通に通したクエリ数 |
| `blocklist_size` | 現在のブロックリストのエントリ数 |

---

## DNS プロバイダーの切り替え（Mac）

```bash
# NextDNS に切り替え（切り替え後、自動で再起動）
make switch-nextdns

# AdGuard DNS Family に切り替え
make switch-adguard
```

| プロバイダー | 無料制限 | 特徴 |
|---|---|---|
| **NextDNS** | 月 30 万クエリ/設定 | ダッシュボードで細かく設定可能。ログも見られる |
| **AdGuard DNS Family** | 無制限 | 登録不要。アダルト・マルウェアを自動ブロック |

---

## よくある質問

**Q. 子供がスマホの設定で DNS を変えて回避できませんか？**
A. ルーターの機能によっては可能です。対策としてルーターで「ポート 53 の外部通信をブロック」するファイアウォールルールを追加するとより確実になります。詳しくはお使いのルーターのマニュアルをご確認ください。

**Q. この PC を切ったらどうなりますか？**
A. セカンダリ DNS に設定した AdGuard DNS が使われます。YouTube のブロックは効かなくなりますが、アダルトコンテンツなどは引き続きブロックされます。

**Q. YouTube アプリ（iPad など）もブロックできますか？**
A. DNS ベースのブロックなのでアプリにも効果があります。ただし YouTube アプリが独自の DNS 設定（DoH など）を使っている場合は回避される可能性があります。

**Q. Mac のソフトウェアアップデート後に動かなくなりました**
A. ターミナルで再ビルドしてみてください。
```bash
cd ~/Documents/my-dns && export PATH="/opt/homebrew/bin:$PATH" && make build
```

**Q. ログはどこで見られますか？**
A. `query.log` ファイルに CSV 形式で記録されています。
```
日時, クライアントIP, ドメイン, クエリタイプ, 結果, 応答時間(ms)
```

---

## ファイル構成

```
my-dns/
├── cmd/
│   ├── dns-server/    # メイン DNS サーバー
│   ├── bl-manager/    # ブロックリスト管理ツール
│   └── log-analyzer/  # ログ解析ツール
├── internal/
│   ├── blocklist/     # ブロックリスト管理
│   ├── cache/         # DNS キャッシュ
│   ├── config/        # 設定ファイル読み込み
│   └── logger/        # クエリログ
├── config.yaml         # アクティブな設定（make switch-* で自動生成）
├── config.nextdns.yaml # NextDNS 用プリセット
├── config.adguard.yaml # AdGuard DNS 用プリセット
├── blocklist.txt       # ブロックするドメインリスト
└── Makefile            # よく使うコマンド集
```

---

## ライセンス

MIT License
