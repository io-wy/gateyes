"""Spike / burst load scenario: sudden 100 QPS for short duration."""
import asyncio
import random

import structlog

from config import CHAT_MODELS, PROMPTS, SPIKE_DURATION_SECONDS, SPIKE_INTERVAL_SECONDS, SPIKE_QPS
from keys import key_pool
from utils import fire_request

logger = structlog.get_logger("loadgen.spike")


async def _single_spike_request(auth: str, model: str, prompt: str):
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "stream": random.random() < 0.5,
        "max_tokens": random.randint(10, 80),
    }
    headers = {
        "Authorization": auth,
        "Content-Type": "application/json",
    }
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        logger.debug("spike_request", model=model, status=resp.status_code)
    except Exception as exc:
        logger.warning("spike_request_error", model=model, error=str(exc))


async def spike_worker():
    """Run a burst every SPIKE_INTERVAL_SECONDS."""
    while True:
        await asyncio.sleep(SPIKE_INTERVAL_SECONDS)
        logger.info("spike_start", target_qps=SPIKE_QPS, duration=SPIKE_DURATION_SECONDS)

        key = key_pool.get("spike")
        auth = key_pool.auth_header(key)

        start = asyncio.get_event_loop().time()
        end = start + SPIKE_DURATION_SECONDS
        sent = 0

        while asyncio.get_event_loop().time() < end:
            # Launch a batch to hit target QPS.
            batch = []
            for _ in range(SPIKE_QPS):
                model = random.choice(CHAT_MODELS)
                prompt = random.choice(PROMPTS)
                batch.append(_single_spike_request(auth, model, prompt))

            # Fire all in parallel, then wait remainder of the second.
            await asyncio.gather(*batch, return_exceptions=True)
            sent += len(batch)

            # Align to next second.
            elapsed = asyncio.get_event_loop().time() - start
            target_time = start + int(elapsed) + 1
            wait = target_time - asyncio.get_event_loop().time()
            if wait > 0:
                await asyncio.sleep(wait)

        logger.info("spike_complete", sent=sent, duration=SPIKE_DURATION_SECONDS)
