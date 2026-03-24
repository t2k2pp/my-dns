BINDIR        := ./bin
CONFDIR       := /usr/local/etc/my-dns
LOGDIR        := /var/log/my-dns
LDFLAGS       := -s -w
LAUNCHD_LABEL := com.mydns.server
LAUNCHD_PLIST := /Library/LaunchDaemons/$(LAUNCHD_LABEL).plist

.PHONY: all build dns-server log-analyzer bl-manager clean run install tidy vet \
        switch-nextdns switch-adguard status reload-blocklist push-blocklist setup-system webui

all: build

## build – compile all three binaries into ./bin/
build: dns-server log-analyzer bl-manager

dns-server:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/dns-server ./cmd/dns-server

log-analyzer:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/log-analyzer ./cmd/log-analyzer

bl-manager:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/bl-manager ./cmd/bl-manager

## setup-system – 初回セットアップ: 設定・ログディレクトリ作成、設定ファイルをシステムへ配置
setup-system:
	sudo mkdir -p $(CONFDIR) $(LOGDIR)
	sudo cp config.yaml config.nextdns.yaml config.adguard.yaml blocklist.txt $(CONFDIR)/
	sudo sh -c 'chmod 640 $(CONFDIR)/*.yaml'
	sudo chmod 644 $(CONFDIR)/blocklist.txt
	sudo chown -R root:wheel $(CONFDIR) $(LOGDIR)
	sudo chmod 750 $(CONFDIR) $(LOGDIR)
	sudo cp com.mydns.server.plist $(LAUNCHD_PLIST)
	@echo "✓ $(CONFDIR) と $(LOGDIR) をセットアップしました"
	@echo "✓ plist を $(LAUNCHD_PLIST) に配置しました"
	@echo ""
	@echo "次のコマンドでサービスを起動してください:"
	@echo "  sudo launchctl load $(LAUNCHD_PLIST)"

## run – start dns-server locally (requires sudo for port 53)
run: build
	sudo $(BINDIR)/dns-server -config $(CONFDIR)/config.yaml

## install – copy binaries to /usr/local/bin (requires sudo)
install: build
	sudo install -m 755 $(BINDIR)/dns-server   /usr/local/bin/dns-server
	sudo install -m 755 $(BINDIR)/log-analyzer /usr/local/bin/log-analyzer
	sudo install -m 755 $(BINDIR)/bl-manager   /usr/local/bin/bl-manager
	@echo "Installed all binaries to /usr/local/bin"

## tidy – update go.sum
tidy:
	go mod tidy

## vet – run go vet on all packages
vet:
	go vet ./...

## switch-nextdns – NextDNS 用設定に切り替えてサービスを再起動
switch-nextdns:
	sudo cp config.nextdns.yaml $(CONFDIR)/config.yaml
	sudo chmod 640 $(CONFDIR)/config.yaml
	@echo "✓ config.yaml を NextDNS 設定に切り替えました"
	@echo "  upstream_dns: $$(sudo grep upstream_dns $(CONFDIR)/config.yaml | awk '{print $$2}')"
	@echo "  block_detect_ips: $$(sudo grep -A2 block_detect_ips $(CONFDIR)/config.yaml | grep '\-' | awk '{print $$2}' | tr '\n' ' ')"
	@if sudo launchctl list $(LAUNCHD_LABEL) > /dev/null 2>&1; then \
		sudo launchctl stop $(LAUNCHD_LABEL) && sleep 1 && sudo launchctl start $(LAUNCHD_LABEL); \
		echo "✓ サービスを再起動しました"; \
	else \
		echo "  (launchd 未登録のためサービス再起動はスキップ)"; \
		echo "  手動起動: make run"; \
	fi

## switch-adguard – AdGuard DNS Family 用設定に切り替えてサービスを再起動
switch-adguard:
	sudo cp config.adguard.yaml $(CONFDIR)/config.yaml
	sudo chmod 640 $(CONFDIR)/config.yaml
	@echo "✓ config.yaml を AdGuard DNS Family 設定に切り替えました"
	@echo "  upstream_dns: $$(sudo grep upstream_dns $(CONFDIR)/config.yaml | awk '{print $$2}')"
	@echo "  block_detect_ips: $$(sudo grep -A3 block_detect_ips $(CONFDIR)/config.yaml | grep '\-' | awk '{print $$2}' | tr '\n' ' ')"
	@if sudo launchctl list $(LAUNCHD_LABEL) > /dev/null 2>&1; then \
		sudo launchctl stop $(LAUNCHD_LABEL) && sleep 1 && sudo launchctl start $(LAUNCHD_LABEL); \
		echo "✓ サービスを再起動しました"; \
	else \
		echo "  (launchd 未登録のためサービス再起動はスキップ)"; \
		echo "  手動起動: make run"; \
	fi

## status – DNS サーバーのメトリクスと現在の設定を表示
status:
	@echo "=== 現在の設定 ($(CONFDIR)/config.yaml) ==="
	@sudo grep upstream_dns $(CONFDIR)/config.yaml 2>/dev/null || echo "  (設定ファイルが見つかりません。make setup-system を実行してください)"
	@sudo grep -A3 block_detect_ips $(CONFDIR)/config.yaml 2>/dev/null || true
	@echo ""
	@echo "=== サーバーメトリクス ==="
	@curl -sf http://127.0.0.1:8080/metrics 2>/dev/null | python3 -m json.tool || echo "  (サーバーが起動していないか管理APIに接続できません)"

## reload-blocklist – サーバーに現在のシステムBLを再読み込みさせる（AUTO_LEARNEDを保持）
## ※ ローカルblocklist.txtの変更を反映したい場合は push-blocklist を使う
reload-blocklist:
	@curl -sf -X POST http://127.0.0.1:8080/reload | python3 -m json.tool
	@echo "✓ ブロックリストをリロードしました"

## push-blocklist – ローカルblocklist.txtをシステムに上書きコピー＋リロード
## ⚠ AUTO_LEARNEDエントリが消えます。手動で追加したドメインを反映する際に使う
push-blocklist:
	sudo cp blocklist.txt $(CONFDIR)/blocklist.txt
	sudo chmod 644 $(CONFDIR)/blocklist.txt
	@curl -sf -X POST http://127.0.0.1:8080/reload | python3 -m json.tool
	@echo "✓ ブロックリストを上書きしました（AUTO_LEARNEDは消えました）"

## webui – Web管理UI を起動 (ログ読み込みのため sudo が必要)
webui:
	@which python3 > /dev/null || (echo "python3 が見つかりません" && exit 1)
	@python3 -c "import flask" 2>/dev/null || (echo "flask が未インストールです。先に実行: pip3 install flask requests" && exit 1)
	cd webui && sudo python3 app.py

## clean – remove build artefacts
clean:
	rm -rf $(BINDIR)
