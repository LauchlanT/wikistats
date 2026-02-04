This repository is for work done for the REDspace backend engineering workshop.

ch-1:

Create a Go application that consumes from an external API, and makes stats about the consumed data available via an API.

ch-2:

Dockerize the application.

ch-3:

Add a ScyllaDB service in its own Docker container, configure Docker Compose to run the API/consumer container and the DB container, update the consumer/API services to optionally use the ScyllaDB database, add authentication to the API, and create integration tests.

ch-4:

Add a Github Actions workflow which triggers on pushes to main. It validates the code with go vet and golangci-lint, runs all unit and integration tests, and pushes the application image to ghcr.io.