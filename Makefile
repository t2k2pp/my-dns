BINDIR   := ./bin
LDFLAGS  := -s -w
LAUNCHD_LABEL := com.mydns.server
LAUNCHD_PLIST := /Library/LaunchDaemons/$(LAUNCHD_LABEL).plist

.PHONY: all build dns-server log-analyzer bl-manager clean run install tidy vet \
        switch-nextdns switch-adguard status reload-blocklist

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

## run – start dns-server (requires config.yaml; uses sudo for port 53)
run: build
	sudo $(BINDIR)/dns-server -config config.yaml

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

## switch-nextdns – config.yaml を NextDNS 用に切り替えてサービスを再起動
switch-nextdns:
	cp config.nextdns.yaml config.yaml
	@echo "✓ config.yaml を NextDNS 設定に切り替えました"
	@echo "  upstream_dns: $$(grep upstream_dns config.yaml | awk '{print $$2}')"
	@echo "  block_detect_ips: $$(grep -A2 block_detect_ips config.yaml | grep '\-' | awk '{print $$2}' | tr '\n' ' ')"
	@if sudo launchctl list $(LAUNCHD_LABEL) > /dev/null 2>&1; then \
		sudo launchctl stop $(LAUNCHD_LABEL) && sleep 1 && sudo launchctl start $(LAUNCHD_LABEL); \
		echo "✓ サービスを再起動しました"; \
	else \
		echo "  (launchd 未登録のためサービス再起動はスキップ)"; \
		echo "  手動起動: sudo ./bin/dns-server -config config.yaml"; \
	fi

## switch-adguard – config.yaml を AdGuard DNS Family 用に切り替えてサービスを再起動
switch-adguard:
	cp config.adguard.yaml config.yaml
	@echo "✓ config.yaml を AdGuard DNS Family 設定に切り替えました"
	@echo "  upstream_dns: $$(grep upstream_dns config.yaml | awk '{print $$2}')"
	@echo "  block_detect_ips: $$(grep -A3 block_detect_ips config.yaml | grep '\-' | awk '{print $$2}' | tr '\n' ' ')"
	@if sudo launchctl list $(LAUNCHD_LABEL) > /dev/null 2>&1; then \
		sudo launchctl stop $(LAUNCHD_LABEL) && sleep 1 && sudo launchctl start $(LAUNCHD_LABEL); \
		echo "✓ サービスを再起動しました"; \
	else \
		echo "  (launchd 未登録のためサービス再起動はスキップ)"; \
		echo "  手動起動: sudo ./bin/dns-server -config config.yaml"; \
	fi

## status – DNS サーバーのメトリクスと現在の設定を表示
status:
	@echo "=== 現在の設定 ==="
	@echo "  プロバイダー upstream_dns: $$(grep upstream_dns config.yaml | awk '{print $$2}')"
	@echo "  block_detect_ips: $$(grep -A3 block_detect_ips config.yaml | grep '\-' | awk '{print $$2}' | tr '\n' ' ')"
	@echo ""
	@echo "=== サーバーメトリクス ==="
	@curl -sf http://127.0.0.1:8080/metrics 2>/dev/null | python3 -m json.tool || echo "  (サーバーが起動していないか管理APIに接続できません)"

## reload-blocklist – blocklist.txt を即時リロード (サーバー再起動不要)
reload-blocklist:
	@curl -sf -X POST http://127.0.0.1:8080/reload | python3 -m json.tool

## clean – remove build artefacts
clean:
	rm -rf $(BINDIR)
