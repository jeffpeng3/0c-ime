.PHONY: help assets ja-import upgrade test-upgrade test-release test-source-release test-windows-package test-sim
.DEFAULT_GOAL := help

PULL ?= 1
# 標準環境變數 LANG 通常已設成語系；這裡只當 make 變數，不匯出給 recipe。
LANG := zh
MODELS ?= 1
MODELS_ONLY ?= 0
JA_OUT ?= build/ja
# 日文來源：空白為 checked-in data/ja（140 筆種子）；指定 build/ja-import 則用 0c-jadic 匯入的完整 pack。
JA_SRC ?=
# 0c-jadic 匯入的來源 pack 與輸出目錄（pack 檔不進 Git）。
JA_PACK ?=
JA_IMPORT ?= build/ja-import

help:
	@echo 'make assets           下載與這份原始碼相符的字典與模型（RELEASE=vX.Y.Z 指定版本，MODELS=0 只取字典）'
	@echo 'make assets MODELS_ONLY=1  只換配對模型，字典須已存在'
	@echo 'make assets LANG=ja   從 data/ja 編譯日文資料包到 build/ja（公開模式）'
	@echo 'make assets LANG=all  同時取得注音字典與模型，並編譯日文資料包'
	@echo 'make assets LANG=ja JA_OUT=build/ja  指定日文資料包輸出目錄'
	@echo 'make ja-import JA_PACK=<ja-lexicon-public-core.pack>  匯入完整公開核心詞典到 build/ja-import'
	@echo 'make assets LANG=ja JA_SRC=build/ja-import  用匯入的詞典產生 Windows 日文包'
	@echo 'make upgrade          更新已安裝的 Linux Fcitx5 輸入法（含 git pull）'
	@echo 'make upgrade PULL=0   使用目前工作目錄，略過 git pull'
	@echo 'make test-release     驗證套件檢查與隔離安裝／回復流程'
	@echo 'make test-source-release  驗證 GitHub 原始碼與模型 tarball 流程'
	@echo 'python3 scripts/source_release.py --help  建立原始碼發佈檔與另附的模型檔'
	@echo 'make build PREFIX=/usr && make install PREFIX=/usr DESTDIR=…  發行版的系統安裝（由套件管理員更新，見 platforms/linux-system）'
	@echo 'python3 scripts/package_release.py --help  建立完整 Linux 套件'
	@echo 'python3 scripts/package_windows.py --help  建立 Windows x64 套件（在 Windows 執行）'
	@echo 'python3 scripts/prepare_windows_runtime.py --help  取出固定版本的 Windows 原生 ONNX Runtime DLL'
	@echo 'make test-windows-package  驗證 Windows 打包、原生 runtime lock 與 Python-free 邊界'
	@echo 'python3 scripts/package_windows_installer.py --help  將 Windows ZIP 製成 EXE 安裝精靈'
	@echo 'go run ./cmd/0c-update status  檢查套件版本'

assets:
	python3 scripts/fetch_assets.py --lang $(LANG) --ja-out $(JA_OUT) $(if $(JA_SRC),--ja-source $(JA_SRC),) \
		$(if $(RELEASE),--release $(RELEASE),) \
		$(if $(filter 1,$(MODELS_ONLY)),--models-only,$(if $(filter 0,$(MODELS)),--no-models,))

# 把外部 0c-jadic pack 轉成執行期布局。connection.tsv 仍取自 data/ja 的 curated 矩陣。
ja-import:
	@test -n "$(JA_PACK)" || { echo '需要 JA_PACK=<ja-lexicon-public-core.pack>；pack 不進 Git' >&2; exit 2; }
	go run ./cmd/0c-jadic import -pack "$(JA_PACK)" -out "$(JA_IMPORT)" -connections data/ja/connection.tsv -force

upgrade:
	python3 scripts/upgrade.py $(if $(filter 0,$(PULL)),--no-pull,)

test-upgrade:
	python3 -m unittest discover -s scripts -p 'test_upgrade.py'

test-sim:
	go test -count=1 -timeout 60m -run TestSimulated ./internal/autoupdate/ -sim.seeds=300 -sim.days=21

test-release:
	python3 -m unittest discover -s scripts -p 'test_release.py'
	python3 -m unittest discover -s platforms/linux-fcitx5/test -p 'l3_result_test.py'
	$(MAKE) test-source-release

test-source-release:
	python3 scripts/check_public_tree.py
	python3 -m unittest discover -s scripts -p 'test_source_release.py'
	python3 -m unittest discover -s scripts -p 'test_copyright.py'

test-windows-package:
	python3 -m unittest discover -s scripts -p 'test_windows_package.py'

# ---- 系統安裝（Linux 發行版套件，#181）----
# make && make install PREFIX=/usr DESTDIR=<暫存根目錄>：把兩種外殼（Fcitx5、IBus）、引擎、0c-update、設定程式、
# 圖示、應用程式選單項目、字典與模型配對清單放到系統目錄，並放上 distribution.json（由套件管理員更新）。
# 不寫安裝收據、不建 systemd 單元、不碰任何家目錄；個人資料照舊在 ~/.local/share/zuin。路徑與相依見 platforms/linux-system/README.md。
PREFIX       ?= /usr/local
BINDIR       ?= $(PREFIX)/bin
LIBDIR       ?= $(PREFIX)/lib
LIBEXECDIR   ?= $(PREFIX)/libexec
DATAROOTDIR  ?= $(PREFIX)/share
MANDIR       ?= $(DATAROOTDIR)/man
DESTDIR      ?=
GO           ?= go
CARGO        ?= cargo
GOBUILDFLAGS ?= -trimpath
VERSION      ?= $(shell git describe --tags --match 'v[0-9]*' 2>/dev/null | sed 's/^v//')
# 字典：0c-ime 原始碼 tarball 的 assets/data（make assets）；也可指向套件或 0c-compile 的輸出。
DATA         ?= assets/data
FCITX5       ?= 1
IBUS         ?= 1
SETTINGS     ?= 1
# 已建好的設定程式（例如 scripts/build_settings_linux.py 的輸出）；空的時候以 cargo 建置。
SETTINGS_BIN ?=
# 模型配對清單（#180）：0c-ime 原始碼 tarball 的 models/pairing.lock.json，綁定 assets/data。0c-update 從 Hugging Face
# 下載模型後在本機配對，結果要等於這份清單；字典不是清單上的那一份（例如自己重建）時下載的模型不會啟用。
PAIRING_LOCK ?= models/pairing.lock.json
# MODEL_DOWNLOAD=0：關掉設定程式的「下載模型」與沒有模型時的提醒（distribution.json 的 model_download）。
MODEL_DOWNLOAD ?= 1
comma := ,

ZC_LIBEXEC   := $(LIBEXECDIR)/0c
ZC_SHARE     := $(DATAROOTDIR)/0c
SYSBUILD     := build/system
GO_MODULE    := $(shell $(GO) list -m 2>/dev/null)
GO_LDFLAGS   := -X main.buildVersion=$(VERSION) -X $(GO_MODULE)/internal/installs.LibexecDir=$(ZC_LIBEXEC) \
                -X $(GO_MODULE)/internal/installs.DataDir=$(ZC_SHARE) -X $(GO_MODULE)/internal/installs.BinDir=$(BINDIR)
# Fcitx5 只從自己的 addon 目錄載入外殼（Debian 是多架構的 /usr/lib/<triplet>/fcitx5）。PREFIX=/usr 時裝到那裡、
# addon 設定寫相對的 Library；其他 PREFIX 裝到 $(LIBDIR)/fcitx5，Library 寫絕對路徑（Fcitx5 不必另外設定）。
FCITX5_SYSTEM_ADDONDIR := $(shell pkg-config --variable=libdir Fcitx5Core 2>/dev/null)/fcitx5
FCITX5_ADDONDIR ?= $(if $(filter /usr,$(PREFIX)),$(FCITX5_SYSTEM_ADDONDIR),$(LIBDIR)/fcitx5)
FCITX5_LIBRARY  := $(if $(filter $(FCITX5_SYSTEM_ADDONDIR),$(FCITX5_ADDONDIR)),lib0c,$(FCITX5_ADDONDIR)/lib0c)
# ibus-daemon 只讀它自己的元件目錄（編譯時的 datadir/ibus/component，Debian 是 /usr/share/ibus/component；
# 設了 IBUS_COMPONENT_PATH 才讀別處），不看 PREFIX：PREFIX=/usr/local 時裝到 /usr/local/share 不會被讀到。
# 所以預設一律裝到 ibus 的目錄（pkg-config ibus-1.0 的 datadir；沒有 libibus-1.0-dev 時是 /usr/share）。
IBUS_DATADIR      := $(or $(shell pkg-config --variable=datadir ibus-1.0 2>/dev/null),/usr/share)
IBUS_COMPONENTDIR ?= $(IBUS_DATADIR)/ibus/component
SETTINGS_OUT := $(if $(SETTINGS_BIN),$(SETTINGS_BIN),$(SYSBUILD)/settings/release/0c-settings)
# man page 的日期：SOURCE_DATE_EPOCH（發行版建置會設，可重現）、HEAD 的 commit 時間，都沒有時用今天。
MAN_DATE ?= $(shell date -u -d @$(or $(SOURCE_DATE_EPOCH),$(shell git log -1 --format=%ct 2>/dev/null),$(shell date +%s)) +%Y-%m-%d)
# 0c-update 的命令列入口 $(BINDIR)/0c-update 是指向 libexec 的相對連結（同在 PREFIX 底下，DESTDIR 暫存樹裡也解得開）。
# PREFIX=~/.local 時那裡是官網安裝的 0c-update：不是指向我們的就不蓋掉、uninstall 也不刪。
ZC_UPDATE_LINK := $(DESTDIR)$(BINDIR)/0c-update
ours_link = [ -L $(ZC_UPDATE_LINK) ] && [ "$$(readlink -m $(ZC_UPDATE_LINK))" = "$$(readlink -m $(DESTDIR)$(ZC_LIBEXEC)/0c-update)" ]

# 從 .in 範本換掉路徑、版本後裝成 644，直接寫到安裝位置：install 不寫建置目錄，sudo make install 才不會留下 root 擁有的檔案。$(1) 範本、$(2) 安裝位置、$(3) sed 參數。
define install_template
install -d $(dir $(DESTDIR)$(2)) && sed $(3) $(1) > $(DESTDIR)$(2) && chmod 644 $(DESTDIR)$(2)
endef

.PHONY: build build-go build-fcitx5 build-settings install uninstall check-version

build: build-go $(if $(filter 1,$(FCITX5)),build-fcitx5) $(if $(filter 1,$(SETTINGS)),$(if $(SETTINGS_BIN),,build-settings))

check-version:
	@test -n '$(VERSION)' || { echo '請指定 VERSION（例如 make VERSION=0.2.3；沒有 git 時無法自動取得）' >&2; exit 1; }

# 0c-update、0c-ibus、0c-mcp 不依賴 C 函式庫（同官網套件）；0c-server 照官網套件以預設方式建置。
build-go: check-version
	@mkdir -p $(SYSBUILD)
	$(GO) build $(GOBUILDFLAGS) -ldflags '$(GO_LDFLAGS)' -o $(SYSBUILD)/0c-server ./cmd/0c-server
	$(foreach b,0c-update 0c-mcp $(if $(filter 1,$(IBUS)),0c-ibus),CGO_ENABLED=0 $(GO) build $(GOBUILDFLAGS) -ldflags '$(GO_LDFLAGS)' -o $(SYSBUILD)/$(b) ./cmd/$(b) &&) true

build-fcitx5: check-version
	$(MAKE) -C platforms/linux-fcitx5 BUILD=$(abspath $(SYSBUILD))/fcitx5 SHELL_VERSION=$(VERSION) \
		ZC_SYSTEM_LIBEXECDIR=$(ZC_LIBEXEC) ZC_SYSTEM_DATADIR=$(ZC_SHARE) $(abspath $(SYSBUILD))/fcitx5/lib0c.so

build-settings: check-version
	cd platforms/settings && ZC_BUILD_VERSION=$(VERSION) ZC_SYSTEM_LIBEXECDIR=$(ZC_LIBEXEC) ZC_SYSTEM_BINDIR=$(BINDIR) \
		CARGO_TARGET_DIR=$(abspath $(SYSBUILD))/settings $(CARGO) build --release --locked --features tauri/custom-protocol

install: check-version
	@test -f '$(DATA)/manifest.json' || { echo '找不到字典 $(DATA)/manifest.json；以 DATA=<字典目錄> 指定（0c-ime 原始碼 tarball 在 assets/data）' >&2; exit 1; }
	@test -x $(SYSBUILD)/0c-server || { echo '請先執行 make build（以同樣的 PREFIX 與 VERSION）' >&2; exit 1; }
	install -Dm755 $(SYSBUILD)/0c-server $(DESTDIR)$(ZC_LIBEXEC)/0c-server
	install -Dm755 $(SYSBUILD)/0c-update $(DESTDIR)$(ZC_LIBEXEC)/0c-update
	install -Dm755 $(SYSBUILD)/0c-mcp $(DESTDIR)$(BINDIR)/0c-mcp
	@# 設定、詞彙、模型的命令列入口要打得到；程式本身留在 libexec，判斷系統安裝時會解開連結
	@if [ -e $(ZC_UPDATE_LINK) -o -L $(ZC_UPDATE_LINK) ] && ! { $(ours_link); }; then \
		echo '$(ZC_UPDATE_LINK) 已是別的 0c-update（例如官網安裝），不覆蓋；系統安裝的在 $(ZC_LIBEXEC)/0c-update' >&2; \
	else install -d $(DESTDIR)$(BINDIR) && ln -sfr $(DESTDIR)$(ZC_LIBEXEC)/0c-update $(ZC_UPDATE_LINK); fi
	$(foreach m,0c-update 0c-mcp,$(call install_template,platforms/linux-system/man/$(m).1.in,$(MANDIR)/man1/$(m).1,-e 's|@VERSION@|$(VERSION)|g' -e 's|@DATE@|$(MAN_DATE)|g') &&) true
	install -d $(DESTDIR)$(ZC_SHARE)/data
	cp -R --no-preserve=mode,ownership '$(DATA)/.' $(DESTDIR)$(ZC_SHARE)/data/
	chmod -R u=rwX,go=rX $(DESTDIR)$(ZC_SHARE)/data
ifneq ($(wildcard $(PAIRING_LOCK)),)
	install -Dm644 '$(PAIRING_LOCK)' $(DESTDIR)$(ZC_SHARE)/neural/pairing.lock.json
	@grep -q "$$(sha256sum '$(DATA)/manifest.json' | cut -d' ' -f1)" '$(PAIRING_LOCK)' || echo '注意：$(PAIRING_LOCK) 不是 $(DATA) 的配對清單，下載的模型不會啟用' >&2
else
	@echo '沒有 $(PAIRING_LOCK)，不提供下載模型（以 PAIRING_LOCK= 指定配對清單，見 scripts/pairing_lock.py）' >&2
endif
	printf '{"schema":1,"updates":"package-manager","version":"%s"$(if $(filter 0,$(MODEL_DOWNLOAD)),$(comma)"model_download":false)}\n' '$(VERSION)' > $(DESTDIR)$(ZC_SHARE)/distribution.json
	chmod 644 $(DESTDIR)$(ZC_SHARE)/distribution.json
	$(foreach i,0c-zhuyin 0c-english,install -Dm644 platforms/linux-fcitx5/icons/$(i).svg $(DESTDIR)$(DATAROOTDIR)/icons/hicolor/scalable/status/$(i).svg &&) true
ifeq ($(FCITX5),1)
	install -Dm644 $(SYSBUILD)/fcitx5/lib0c.so $(DESTDIR)$(FCITX5_ADDONDIR)/lib0c.so
	$(call install_template,platforms/linux-fcitx5/0c-addon.conf,$(DATAROOTDIR)/fcitx5/addon/0c.conf,'s|^Library=lib0c$$|Library=$(FCITX5_LIBRARY)|')
	install -Dm644 platforms/linux-fcitx5/0c-im.conf $(DESTDIR)$(DATAROOTDIR)/fcitx5/inputmethod/zuin.conf
endif
ifeq ($(IBUS),1)
	install -Dm755 $(SYSBUILD)/0c-ibus $(DESTDIR)$(ZC_LIBEXEC)/0c-ibus
	$(call install_template,platforms/linux-ibus/0c.xml.in,$(IBUS_COMPONENTDIR)/0c.xml,-e 's|@LIBEXECDIR@|$(ZC_LIBEXEC)|g' -e 's|@DATAROOTDIR@|$(DATAROOTDIR)|g')
endif
ifeq ($(SETTINGS),1)
	install -Dm755 $(SETTINGS_OUT) $(DESTDIR)$(BINDIR)/0c-settings
	install -Dm644 platforms/settings/icons/128x128.png $(DESTDIR)$(DATAROOTDIR)/icons/hicolor/128x128/apps/0c-settings.png
	$(call install_template,platforms/settings/0c-settings.desktop.in,$(DATAROOTDIR)/applications/0c-settings.desktop,'s|@BINDIR@|$(BINDIR)|g')
endif

uninstall:
	if $(ours_link); then rm -f $(ZC_UPDATE_LINK); fi
	rm -f $(DESTDIR)$(ZC_LIBEXEC)/0c-server $(DESTDIR)$(ZC_LIBEXEC)/0c-update $(DESTDIR)$(ZC_LIBEXEC)/0c-ibus \
	      $(DESTDIR)$(BINDIR)/0c-mcp $(DESTDIR)$(BINDIR)/0c-settings $(DESTDIR)$(ZC_SHARE)/distribution.json \
	      $(DESTDIR)$(MANDIR)/man1/0c-update.1 $(DESTDIR)$(MANDIR)/man1/0c-mcp.1 \
	      $(DESTDIR)$(ZC_SHARE)/neural/sequence_runtime.py $(DESTDIR)$(ZC_SHARE)/neural/punct_runtime.py $(DESTDIR)$(ZC_SHARE)/neural/pairing.lock.json \
	      $(DESTDIR)$(FCITX5_ADDONDIR)/lib0c.so $(DESTDIR)$(DATAROOTDIR)/fcitx5/addon/0c.conf \
	      $(DESTDIR)$(DATAROOTDIR)/fcitx5/inputmethod/zuin.conf $(DESTDIR)$(IBUS_COMPONENTDIR)/0c.xml \
	      $(DESTDIR)$(DATAROOTDIR)/applications/0c-settings.desktop $(DESTDIR)$(DATAROOTDIR)/icons/hicolor/128x128/apps/0c-settings.png \
	      $(DESTDIR)$(DATAROOTDIR)/icons/hicolor/scalable/status/0c-zhuyin.svg $(DESTDIR)$(DATAROOTDIR)/icons/hicolor/scalable/status/0c-english.svg
	rm -rf $(DESTDIR)$(ZC_SHARE)/data
	-rmdir $(DESTDIR)$(ZC_SHARE)/neural $(DESTDIR)$(ZC_SHARE) $(DESTDIR)$(ZC_LIBEXEC) 2>/dev/null
	-rmdir $(DESTDIR)$(DATAROOTDIR)/fcitx5/inputmethod $(DESTDIR)$(DATAROOTDIR)/fcitx5/addon 2>/dev/null # 只刪空的：install 時建了，別的套件的檔案還在就留著
