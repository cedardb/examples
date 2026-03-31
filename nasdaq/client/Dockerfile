FROM ubuntu:25.04

# Install dependencies
RUN apt-get update && apt-get install -y g++ cmake libpq-dev

# Build client
COPY . .
RUN cmake . -DCMAKE_BUILD_TYPE=Release -B bin && cmake --build bin

ENV DB_HOST=cedar
ENV DB_PORT=5432
ENV DB_USER=postgres
ENV DB_PASSWORD=postgres
ENV CEDAR_DATA_DIR=/data/
ENV CLIENT_DATA_DIR=/nasdaq/data/

# Start the client on container start
CMD ["sh", "-c", "exec ./bin/NasdaqDriver \"$DB_HOST\" \"$DB_PORT\" \"$DB_USER\" \"$DB_PASSWORD\" \"$CEDAR_DATA_DIR\" \"$CLIENT_DATA_DIR\""]
