"""Pure chat scenario: streaming + multi-model mix."""
import asyncio
import json
import random

import structlog

from config import CHAT_MODELS, PROMPTS, SCENARIO_QPS
from keys import key_pool
from utils import fire_request, sleep_for_qps

logger = structlog.get_logger("loadgen.chat")


async def _stream_chat(model: str, prompt: str, auth: str):
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "stream": True,
        "max_tokens": random.randint(20, 150),
    }
    headers = {
        "Authorization": auth,
        "Content-Type": "application/json",
        "Accept": "text/event-stream",
    }
    client = __import__("utils", fromlist=["get_client"]).get_client()
    url = f"{__import__('config').GATEWAY}/v1/chat/completions"
    try:
        async with client.stream("POST", url, headers=headers, json=payload, timeout=60) as resp:
            chunks = 0
            async for _ in resp.aiter_lines():
                chunks += 1
            logger.debug("stream_finished", model=model, chunks=chunks, status=resp.status_code)
    except Exception as exc:
        logger.warning("stream_error", model=model, error=str(exc))


async def _non_stream_chat(model: str, prompt: str, auth: str):
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "stream": False,
        "max_tokens": random.randint(20, 150),
    }
    headers = {
        "Authorization": auth,
        "Content-Type": "application/json",
    }
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        if resp.status_code == 200:
            body = resp.json()
            usage = body.get("usage", {})
            logger.debug(
                "chat_ok",
                model=model,
                prompt_tokens=usage.get("prompt_tokens", 0),
                completion_tokens=usage.get("completion_tokens", 0),
            )
        else:
            logger.debug("chat_non_200", model=model, status=resp.status_code, body=resp.text[:200])
    except Exception as exc:
        logger.warning("chat_error", model=model, error=str(exc))


async def chat_worker():
    """Main worker loop for chat scenario."""
    target_qps = SCENARIO_QPS["chat"]
    while True:
        try:
            key = key_pool.get("chat")
            auth = key_pool.auth_header(key)
            model = random.choice(CHAT_MODELS)
            prompt = random.choice(PROMPTS)
            if random.random() < 0.7:
                await _stream_chat(model, prompt, auth)
            else:
                await _non_stream_chat(model, prompt, auth)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.exception("chat_worker_exception", error=str(exc))
        await asyncio.sleep(sleep_for_qps(target_qps))
