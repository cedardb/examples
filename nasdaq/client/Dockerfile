FROM ubuntu:26.04

# Install dependencies
RUN apt-get update && apt-get install -y g++ cmake libpq-dev

# Build client
COPY . .
RUN cmake . -DCMAKE_BUILD_TYPE=Release -B bin && cmake --build bin

ENV DB1_CONNECTION_STRING=postgresql://postgres:postgres@cedar:5432/postgres
ENV DB2_CONNECTION_STRING=

# Start the client on container start
CMD ["sh", "-c", "exec ./bin/NasdaqDriver /nasdaq/data/ /data/ \"$DB1_CONNECTION_STRING\" \"$DB2_CONNECTION_STRING\""]
