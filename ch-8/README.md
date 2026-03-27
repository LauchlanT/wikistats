A Docker application to consume data on recent Wikipedia changes from https://stream.wikimedia.org/v2/stream/recentchange and provide an API to view stats about the consumed streams. The application uses Redpanda to separate the producer, which pulls from the stream, and the consumer, which pushes to the database.

## Chapter 8 notes

### Threading

While the consumer had been multi-threaded previously, spawning multiple Goroutines which process messages pulled from Redpanda,this is no longer the case. The requirement to batch writes to the database makes multi-threading dangerous. It creates a risk of threads committing batches that increment the partition offset, meanwhile earlier messages in the partition fail processing and then never get re-processed because the offset is past them. For this application a safer and more idiomatic approach is taken, limiting the consumer to a single thread but instantiating a number of consumers equal to the number of partitions in the Redpanda topic. This way each consumer will pull messages from only one partition, and there's no risk of out-of-order consumption. To validate that there are no race conditions, an additional test has been added in cmd/consumer, which spawns multiple consumers and has them read 10000 messages, and validates that all messages are consumed and the final stats generated are correct. A dead letter queue has been created so if a message cannot be processed it will be sent to the DLQ and the consumer can continue to pull new messages from its partition.

### Batching

For the in-memory database batching is very straightforward, we just hold the lock on the database until all records from the batch have been inserted. Since there's no possibility of an insert failing, there are no concerns about failures mid-batch.

For ScyllaDB batching is a serious challenge. The light weight transactions (LWTs) needed to ensure that duplicate values aren't inserted into the database do not really support batching - if a batch is inserted and any record is fails to insert because of an existing record, the entire batch is rejected. Therefore for ScyllaDB there needs to be an iterative fall-back that inserts records one by one if the batch insert fails. However, this does mean that there are more potential failure cases. Here are some potential risk:

1. If the batch is insert successfully, we still need to go and increment the counts for each statistic, unfortunately this cannot be included with the batch. Therefore there is a chance that the batch inserts all the values successfully, but one or more increments fail, and therefore the counts do not accurately reflect the state of the database. There is no way to roll-back the insert as it's possible that other processes may have inserted the same value, so deleting it may not fix the issue. Therefore we just log the failed increment, and in the future the database could perhaps have a process run to correct the counts. If correctness is critical at all times, batching cannot be used without serious efficiency loss and should be reverted.

2. If the batch does not insert, we insert each record individually. If any of these records fail, the process will exit early and return the number of records correctly stored. Again, during this process the increment cannot happen alongside the insert, so there is the possibility of an increment failing after a successful insert - it is less bad since it's just one record but it is still an issue. If this level of incorrectness is not acceptable, do not use ScyllaDB for this kind of thing. Otherwise, again the solution will be to run a clean-up process on the database periodically if these errors occur.

3. If the batch is only partially inserted, the consumer recognizes that only some records have been processed and only commits the records which have been stored to the database. The next record in the batch is sent to the DLQ so it does not block further processing of messages in the partition. This could cause issues if the database is down and processing fails because of this - then all messages could be sent to the DLQ. However, ScyllaDB is designed to be highly available, so this is an acceptable risk in this application.

## Running

### 1. Docker compose (with in-memory database):

Start the application with ```docker compose -f deployment/docker-compose.yml up --scale wikistats-consumer=6 --build -d```

Stop the application with ```docker compose -f deployment/docker-compose.yml down -v```

### 2. Docker compose (with ScyllaDB database):

Edit the .env file and add ```DATABASE_TYPE=scylla```

Start the application and a 3 node ScyllaDB cluster with ```docker compose -f deployment/docker-compose.yml --profile scylla up --scale wikistats-consumer=6 --build -d```

Stop the application with ```docker compose -f deployment/docker-compose.yml --profile scylla down``` (or ```docker compose -f deployment/docker-compose.yml --profile scylla down -v``` to clear the database)

## Testing

The application has both unit tests and integration tests, with several options to run them.

### 1. Run all tests on Docker

To run tests as comprehensively as possible, there is a docker-compose-test.yml file that sets up a testing environment and runs both unit and integration tests. It also runs a simple end-to-end test which validates the entire stack, including the metrics from Prometheus. Note that the services are configured to use .test_env for their environment, and settings can be adjusted as desired with that file.

Start the test system with ```docker compose -f deployment/docker-compose-test.yml up -d```

Once all services have started, monitor the logs with ```docker compose -f deployment/docker-compose-test.yml logs -f test-runner```

After all tests have completed, shut down the test system with ```docker compose -f deployment/docker-compose-test.yml down -v```

### 2. Run unit tests locally

Unit tests don't require spinning up the ScyllaDB cluster or Redpanda and can be run with ```go test -v -tags=unit ./...```

### 3. Run integration tests locally

Integration tests can be run against a running ScyllaDB cluster, but you must first edit .test-env to set ```SCYLLA_HOSTS=localhost```, and the Scylla cluster and Redpanda node from docker-compose-test.yml must be running.

Then you can run ```go test -v -tags=integration ./...```

## Using

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

```http://localhost:8080``` can be visited to view the Redpanda console.

```http://localhost:3000``` can be visited to view the Grafana dashboard. The user/password is admin/admin.

## Workflows

Pushes to main will result in the build.yml workflow running, which validates the code with got vet and golangci-lint, runs all tests using docker-compose-test.yml, and pushes the application's image to ghcr.io/lauchlant/wikistats
