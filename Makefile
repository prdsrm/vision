service = "moonbot"
version = "latest"
command = "worker"
server = "server-1"
container = "project-db-1"

### Docker command

docker-run:
	docker compose --env-file .env.dev up $(service)

docker-build:
	docker build --target=prod -t vision:latest .

# NOTE: workaround, for MacOS users not using Asahi Linux.
colima:
	colima start --cpu 6 --memory 6 --disk 100
macos-trick:
	colima stop
	colima start --cpu 6 --memory 6 --disk 100
docker-build-arm:
	docker build --target=prod --build-arg GOARCH=arm64 -t vision:latest .

#### Docker compose

docker-compose-setup-local:
	docker compose up -d db

docker-compose-remote-show-container:
	docker --context remote ps -a

docker-create-remote-context:
	docker context create remote --docker "host=ssh://$(server)"

docker-remote-update-image:
	docker save $(image):latest | ssh $(server) -C docker load

docker-compose-deploy-remote:
	docker --context remote compose up -d $(service)

docker-compose-remote-stop:
	docker --context remote compose down

### Working with database

db-prompt:
	docker exec -it $(container) psql $(DATABASE_URL)
	
remote-db-prompt:
	docker --context remote exec -it $(container) psql $(DATABASE_URL)
