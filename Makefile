COMPOSE_DEV := docker compose -f docker-compose.yml -f docker-compose.dev.yml
COMPOSE_PROD := docker compose -f docker-compose.yml -f docker-compose.prod.yml
DB_DUMP_DIR ?= backups
DB_DUMP_FILE ?= $(DB_DUMP_DIR)/postgres-$(shell date +%Y%m%d-%H%M%S).dump
DB_RESTORE_FILE ?=

.PHONY: help
help:
	@echo "使い方:"
	@echo "  make version          version.full(X.X.X-{commitHash})を現在のHEADで生成"
	@echo "  make dev-up           Viteフロントエンドを含む開発用スタックを起動"
	@echo "  make dev-down         開発用スタックを停止"
	@echo "  make dev-logs         開発用スタックのログを表示"
	@echo "  make dev-ps           開発用コンテナの状態を表示"
	@echo "  make prod-up          nginxフロントエンドを含む本番用スタックを起動"
	@echo "  make prod-down        本番用スタックを停止"
	@echo "  make prod-logs        本番用スタックのログを表示"
	@echo "  make prod-ps          本番用コンテナの状態を表示"
	@echo "  make backend-test     Goバックエンドのテストを実行"
	@echo "  make frontend-build   フロントエンドをビルド"
	@echo "  make sqlc-generate    sqlcの生成コードを再生成"
	@echo "  make vapid-keygen     Web Push用のVAPID鍵を生成し環境変数を出力"
	@echo "  make db-shell         開発用PostgreSQLでpsqlを開く"
	@echo "  make dev-db-dump      開発用PostgreSQLをdump"
	@echo "  make dev-db-restore   開発用PostgreSQLへrestore"
	@echo "  make prod-db-dump     本番用PostgreSQLをdump"
	@echo "  make prod-db-restore  本番用PostgreSQLへrestore"
	@echo "  make frontend-mock    フロントエンドモックモード"
	@echo "  make valkey-cli       開発用Valkeyでvalkey-cliを開く"
	@echo "  make clean            ローカルのビルド出力とキャッシュを削除"
	@echo "  make clean-volumes    スタックを停止し、Docker volumeも削除"
	@echo ""
	@echo "dump/restore:"
	@echo "  dump先の変更:     make dev-db-dump DB_DUMP_FILE=backups/app.dump"
	@echo "  restore元の指定:  make dev-db-restore DB_RESTORE_FILE=backups/app.dump"

# コミット対象の version（X.X.X のみ）から、commitHash付きの version.full
# （X.X.X-{commitHash}、git管理外）を生成する。これにより、コミットされる
# version ファイルがブランチ/コミットハッシュとずれない。
# 各ビルドの前提（prerequisite）にも入れているため、ビルド前に必ず最新化される。
.PHONY: version
version:
	@base=$$(tr -d '[:space:]' < version); \
	hash=$$(git rev-parse --short HEAD 2>/dev/null || echo nogit); \
	printf '%s-%s\n' "$$base" "$$hash" > version.full; \
	echo "version.full: $$(cat version.full)"

.PHONY: dev-up dev-down dev-build dev-logs dev-ps dev-restart
dev-up: version
	$(COMPOSE_DEV) up --build

dev-down:
	$(COMPOSE_DEV) down

dev-build: version
	$(COMPOSE_DEV) build

dev-logs:
	$(COMPOSE_DEV) logs -f

dev-ps:
	$(COMPOSE_DEV) ps

dev-restart:
	$(COMPOSE_DEV) restart

.PHONY: prod-up prod-down prod-build prod-logs prod-ps prod-restart
prod-up: version
	$(COMPOSE_PROD) up --build -d

prod-down:
	$(COMPOSE_PROD) down

prod-build: version
	$(COMPOSE_PROD) build

prod-logs:
	$(COMPOSE_PROD) logs -f

prod-ps:
	$(COMPOSE_PROD) ps

prod-restart:
	$(COMPOSE_PROD) restart

.PHONY: backend-test frontend-build sqlc-generate
backend-test:
	cd backend && go test ./...

frontend-build: version
	cd frontend && npm ci && npm run build

sqlc-generate:
	cd backend && sqlc generate

# Web Push 用の VAPID 鍵を単発コンテナで生成し、設定すべき環境変数を標準出力へ出す。
.PHONY: vapid-keygen
vapid-keygen:
	$(COMPOSE_DEV) run --rm --no-deps backend /bin/vapid-keygen

.PHONY: db-shell dev-db-dump dev-db-restore prod-db-dump prod-db-restore valkey-cli
db-shell:
	$(COMPOSE_DEV) exec postgres psql -U app -d app

dev-db-dump:
	mkdir -p $$(dirname "$(DB_DUMP_FILE)")
	$(COMPOSE_DEV) exec -T postgres pg_dump -U app -d app -Fc > $(DB_DUMP_FILE)
	@echo "開発用PostgreSQL dumpを作成しました: $(DB_DUMP_FILE)"

dev-db-restore:
	@test -n "$(DB_RESTORE_FILE)" || (echo "DB_RESTORE_FILE=path/to/dump を指定してください"; exit 1)
	@test -f "$(DB_RESTORE_FILE)" || (echo "restore元ファイルが見つかりません: $(DB_RESTORE_FILE)"; exit 1)
	$(COMPOSE_DEV) exec -T postgres pg_restore -U app -d app --clean --if-exists --no-owner < $(DB_RESTORE_FILE)
	@echo "開発用PostgreSQLへrestoreしました: $(DB_RESTORE_FILE)"

prod-db-dump:
	mkdir -p $$(dirname "$(DB_DUMP_FILE)")
	$(COMPOSE_PROD) exec -T postgres pg_dump -U app -d app -Fc > $(DB_DUMP_FILE)
	@echo "本番用PostgreSQL dumpを作成しました: $(DB_DUMP_FILE)"

prod-db-restore:
	@test -n "$(DB_RESTORE_FILE)" || (echo "DB_RESTORE_FILE=path/to/dump を指定してください"; exit 1)
	@test -f "$(DB_RESTORE_FILE)" || (echo "restore元ファイルが見つかりません: $(DB_RESTORE_FILE)"; exit 1)
	$(COMPOSE_PROD) exec -T postgres pg_restore -U app -d app --clean --if-exists --no-owner < $(DB_RESTORE_FILE)
	@echo "本番用PostgreSQLへrestoreしました: $(DB_RESTORE_FILE)"

frontend-mock:
	cd frontend && VITE_MOCK_USER=1 npm run dev

valkey-cli:
	$(COMPOSE_DEV) exec valkey valkey-cli

.PHONY: clean clean-volumes
clean:
	rm -rf backend/.gocache backend/.gomodcache frontend/dist frontend/node_modules frontend/.npm-cache frontend/tsconfig.tsbuildinfo

clean-volumes:
	$(COMPOSE_DEV) down -v
	$(COMPOSE_PROD) down -v
