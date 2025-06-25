FROM python:3.11-slim
WORKDIR /app
COPY . /app
RUN apt-get update && apt-get --yes --no-install-recommends install python3-dev build-essential cmake curl && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 1999
ENTRYPOINT [ "bash", "./docker_startup.sh" ]

