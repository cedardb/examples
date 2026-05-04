FROM ubuntu:25.10

# Install dependencies
RUN apt-get update && apt-get install -y g++ cmake libpq-dev

# Build client
COPY . .
RUN cmake . -DCMAKE_BUILD_TYPE=Release -B bin && cmake --build bin

# Start the client on container start
CMD ["./bin/NasdaqDriver", "/nasdaq/data/", "/data/", "postgresql://postgres:postgres@cedar:5432/postgres"]
