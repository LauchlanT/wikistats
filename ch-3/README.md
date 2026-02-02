A Docker application to consume data on recent Wikipedia changes from https://stream.wikimedia.org/v2/stream/recentchange and provide an API to view stats about the consumed streams.

##Running

There are now multiple ways to run the application

###1. Dockerfile (with in-memory database): 

Build the Docker image with ```docker build -t wikistats:latest .```

Run the container with ```docker run -d --rm -p 7000:7000 --name wikistats wikistats:latest``` (or change the port number if you've changed the .env API_PORT value)

Stop the container with ```docker stop wikistats```

###2. Docker compose (with in-memory database):

Start the application with ```docker compose up wikistats --build -d```

Stop the application with ```docker compose down wikistats -v```

###3. Docker compose (with ScyllaDB database):

Edit the .env file and change ```DATABASE_TYPE=inmemory``` to ```DATABASE_TYPE=scylla```

Start the application and a 3 node ScyllaDB cluster with ```docker compose --profile scylla up --build -d```

Stop the application with ```docker compose --profile scylla down``` (or ```docker compose --profile scylla down -v``` to clear the database)

##Testing

The application has both unit tests and integration tests, with several options to run them.

###1. Run all tests on Docker

To run tests as comprehensively as possible, there is a docker-compose-test.yml file that sets up a testing environment and runs both unit and integration tests.

Start the test system with ```docker compose -f docker-compose-test.yml up -d```

Once all services have started, monitor the logs with ```docker compose -f docker-compose-test.yml logs -f test-runner```

After all tests have completed, shut down the test system with ```docker compose -f docker-compose-test.yml down -v```

###2. Run unit tests locally

Unit tests don't require spinning up the ScyllaDB cluster and can be run with ```go test -v -tags=unit ./...```

###3. Run integration tests locally

Integration tests can be run against a running ScyllaDB cluster, but you must first edit .test-env to set ```SCYLLA_HOSTS=localhost```, and the Scylla cluster from docker-compose-test.yml must be running.

Then you can run ```go test -v -tags=integration ./...```

##Using

The application now has authentication requiring bearer tokens, so a tool like Postman or Bruno is recommended.

Hitting ```http://localhost:7000/healthcheck``` still works in browser or in any HTTP request tool without authentication, displaying if the service is active.

To log in, send a POST request to ```http://localhost:7000/login``` and send the JSON body 

```
{
  "username":"admin",
  "password":"admin"
}
```

The login endpoint will return a string upon successful login - copy this as the bearer token sent in authenticated requests.

```http://localhost:7000/stats``` requires the inclusion of the bearer token to view the statistics collected by the application.

```http://localhost:7000/logout``` can also be hit to revoke the bearer token the request is sent with.
