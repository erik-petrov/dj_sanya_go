"""Speech-to-text sidecar for the Discord bot's voice-command feature.

The bot (ears session) receives decrypted Opus frames from Discord and POSTs a
whole utterance here as a length-prefixed blob:

    [uint16 big-endian frame length][frame bytes] ... repeated

We decode the Opus (48 kHz stereo, 20 ms frames), downmix to mono, resample to
16 kHz, and run faster-whisper (Russian) with Silero VAD. Returns {"text": ...}.
"""
import os
import struct
import threading

import numpy as np
import opuslib
import soxr
from fastapi import FastAPI, Request
from faster_whisper import WhisperModel
from starlette.concurrency import run_in_threadpool

MODEL = os.getenv("WHISPER_MODEL", "small")
LANG = os.getenv("WHISPER_LANG", "ru")
DEVICE = os.getenv("WHISPER_DEVICE", "cpu")
COMPUTE = os.getenv("WHISPER_COMPUTE", "int8")

SAMPLE_RATE = 48000          # Discord voice sample rate
CHANNELS = 2                 # Discord sends stereo
FRAME_SAMPLES = 960          # 20 ms @ 48 kHz, per channel
WHISPER_RATE = 16000         # what Whisper expects

app = FastAPI()
model = WhisperModel(MODEL, device=DEVICE, compute_type=COMPUTE)
decoder = opuslib.Decoder(SAMPLE_RATE, CHANNELS)


def decode_utterance(body: bytes) -> np.ndarray:
    """Parse the length-prefixed Opus frames and return mono 16 kHz float32."""
    pcm = bytearray()
    i, n = 0, len(body)
    while i + 2 <= n:
        (flen,) = struct.unpack_from(">H", body, i)
        i += 2
        if flen == 0 or i + flen > n:
            break
        frame = bytes(body[i:i + flen])
        i += flen
        try:
            pcm += decoder.decode(frame, FRAME_SAMPLES)
        except Exception:
            # Skip undecodable frames rather than abort the whole utterance.
            continue

    if not pcm:
        return np.zeros(0, dtype=np.float32)

    samples = np.frombuffer(bytes(pcm), dtype=np.int16).astype(np.float32) / 32768.0
    samples = samples.reshape(-1, CHANNELS).mean(axis=1)  # stereo -> mono
    return soxr.resample(samples, SAMPLE_RATE, WHISPER_RATE).astype(np.float32)


# The Whisper model and Opus decoder aren't thread-safe, and inference is
# CPU-bound. Serialize it under a lock and run it via run_in_threadpool so the
# blocking work never stalls the async event loop (which would make the bot's
# HTTP client time out waiting to even connect).
_infer_lock = threading.Lock()


def _run_stt(body: bytes):
    with _infer_lock:
        audio = decode_utterance(body)
        if audio.size < WHISPER_RATE * 0.3:  # < 0.3 s of audio, ignore
            return "", LANG
        segments, info = model.transcribe(
            audio,
            language=LANG,
            beam_size=5,
            temperature=0.0,
            # Commands are independent utterances; don't let the model "continue"
            # into invented text (kills hallucinated tails on trailing silence).
            condition_on_previous_text=False,
            vad_filter=True,
            vad_parameters=dict(min_silence_duration_ms=500),
            no_speech_threshold=0.6,
            log_prob_threshold=-1.0,
        )
        text = " ".join(s.text.strip() for s in segments).strip()
        return text, info.language


@app.post("/transcribe")
async def transcribe(request: Request):
    body = await request.body()
    text, lang = await run_in_threadpool(_run_stt, body)
    return {"text": text, "language": lang}


@app.get("/health")
async def health():
    return {"ok": True, "model": MODEL, "lang": LANG}
