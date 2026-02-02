.PHONY: dev up down logs

dev:
	docker compose -f docker-compose.dev.yml up --build -d

prod:
	docker compose up --build

stop:
	docker compose down

logs:
	docker compose logs -f
