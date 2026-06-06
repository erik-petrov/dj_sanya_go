FROM python:3.11-slim

# libopus0 is needed by opuslib to decode the Opus frames.
RUN apt-get update \
    && apt-get install -y --no-install-recommends libopus0 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .

ENV WHISPER_MODEL=small
EXPOSE 8000

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
