A Docker application to consume data on recent Wikipedia changes from https://stream.wikimedia.org/v2/stream/recentchange and provide an API to view stats about the consumed streams.

Build the Docker image with ```docker build -t wikistats:latest .```

Run the container with ```docker run -d --rm -p 7000:7000 --name wikistats wikistats:latest``` (or change the port number if you've changed the .env API_PORT value)

Stop the container with ```docker stop wikistats```

View the stats at localhost:7000/stats

Verify that the application is running at localhost:7000/healthcheck
