.PHONY: dev backend frontend install

dev:
	@make -j 2 backend frontend

backend:
	@echo "Starting backend with air..."
	@cd backend && air

frontend:
	@echo "Starting frontend with next dev..."
	@cd frontend && npm run dev

install:
	@echo "Installing backend dependencies..."
	@cd backend && go mod tidy
	@echo "Installing frontend dependencies..."
	@cd frontend && pnpm install --registry=https://registry.npmmirror.com

