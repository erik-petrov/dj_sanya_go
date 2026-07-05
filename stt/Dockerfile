FROM python:3.11-slim

# libopus0: Opus decode (opuslib). ffmpeg + libsndfile1: audio I/O for GigaAM/soundfile.
RUN apt-get update \
    && apt-get install -y --no-install-recommends libopus0 ffmpeg libsndfile1 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# CPU-only torch/torchaudio — the default wheels pull multi-GB CUDA libraries.
RUN pip install --no-cache-dir torch torchaudio --index-url https://download.pytorch.org/whl/cpu
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .

ENV STT_ENGINE=gigaam
EXPOSE 8000

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
