"""Speech-to-text sidecar for the Discord bot's voice-command feature.

The bot (ears session) receives decrypted Opus frames from Discord and POSTs a
whole utterance here as a length-prefixed blob:

    [uint16 big-endian frame length][frame bytes] ... repeated

We decode the Opus (48 kHz stereo, 20 ms frames), downmix to mono and resample
to 16 kHz, then transcribe with the selected engine:

  STT_ENGINE=gigaam   -> GigaAM v3 (Russian-specialised, CPU-friendly)  [default]
  STT_ENGINE=whisper  -> faster-whisper (multilingual fallback)

Returns {"text": ..., "language": ...}.
"""
import os
import struct
import tempfile
import threading

import numpy as np
import opuslib
import soundfile as sf
import soxr
from fastapi import FastAPI, Request
from starlette.concurrency import run_in_threadpool

ENGINE = os.getenv("STT_ENGINE", "gigaam").lower()
LANG = os.getenv("STT_LANG", "ru")

SAMPLE_RATE = 48000   # Discord voice sample rate
CHANNELS = 2          # Discord sends stereo
FRAME_SAMPLES = 960   # 20 ms @ 48 kHz, per channel
TARGET_RATE = 16000   # what the ASR models expect

app = FastAPI()
decoder = opuslib.Decoder(SAMPLE_RATE, CHANNELS)

# ---- engine setup (only the selected one is loaded) ----
if ENGINE == "gigaam":
    import gigaam

    _gigaam = gigaam.load_model(
        os.getenv("GIGAAM_MODEL", "v2_ctc"),
        fp16_encoder=False,  # CPU: keep fp32
        device="cpu",
    )
    _whisper = None
else:
    from faster_whisper import WhisperModel

    _whisper = WhisperModel(
        os.getenv("WHISPER_MODEL", "base"),
        device=os.getenv("WHISPER_DEVICE", "cpu"),
        compute_type=os.getenv("WHISPER_COMPUTE", "int8"),
    )
    _gigaam = None


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
    return soxr.resample(samples, SAMPLE_RATE, TARGET_RATE).astype(np.float32)


def _transcribe_gigaam(audio: np.ndarray) -> str:
    # GigaAM takes a file path, so write a 16 kHz mono WAV to a temp file.
    fd, path = tempfile.mkstemp(suffix=".wav")
    os.close(fd)
    try:
        sf.write(path, audio, TARGET_RATE, subtype="PCM_16")
        return (_gigaam.transcribe(path) or "").strip()
    finally:
        try:
            os.remove(path)
        except OSError:
            pass


def _transcribe_whisper(audio: np.ndarray) -> str:
    segments, _ = _whisper.transcribe(
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
    return " ".join(s.text.strip() for s in segments).strip()


# Models aren't thread-safe and inference is CPU-bound: serialize under a lock
# and run via run_in_threadpool so the blocking work never stalls the async
# event loop (which would make the bot's HTTP client time out connecting).
_infer_lock = threading.Lock()


def _run_stt(body: bytes) -> str:
    with _infer_lock:
        audio = decode_utterance(body)
        if audio.size < TARGET_RATE * 0.3:  # < 0.3 s of audio, ignore
            return ""
        if ENGINE == "gigaam":
            return _transcribe_gigaam(audio)
        return _transcribe_whisper(audio)


@app.post("/transcribe")
async def transcribe(request: Request):
    body = await request.body()
    text = await run_in_threadpool(_run_stt, body)
    return {"text": text, "language": LANG}


@app.get("/health")
async def health():
    return {"ok": True, "engine": ENGINE, "lang": LANG}
