"""Bad request / error injection scenario."""
import asyncio
import json
import random

import structlog

from config import CHAT_MODELS, SCENARIO_QPS
from keys import key_pool
from utils import fire_request, sleep_for_qps

logger = structlog.get_logger("loadgen.bad")

BAD_CASES = [
    "invalid_json",
    "oversized_tokens",
    "unknown_model",
    "empty_messages",
    "revoked_key",
    "missing_auth",
    "bad_content_type",
]


async def _bad_invalid_json(auth: str):
    headers = {"Authorization": auth, "Content-Type": "application/json"}
    client = __import__("utils", fromlist=["get_client"]).get_client()
    url = f"{__import__('config').GATEWAY}/v1/chat/completions"
    try:
        resp = await client.post(url, headers=headers, content=b"{not json")
        logger.debug("bad_invalid_json", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_invalid_json_exc", error=str(exc))


async def _bad_oversized_tokens(auth: str):
    payload = {
        "model": random.choice(CHAT_MODELS),
        "messages": [{"role": "user", "content": "hi"}],
        "max_tokens": 999999999,
    }
    headers = {"Authorization": auth, "Content-Type": "application/json"}
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        logger.debug("bad_oversized", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_oversized_exc", error=str(exc))


async def _bad_unknown_model(auth: str):
    payload = {
        "model": "this-model-does-not-exist-" + str(random.randint(1000, 9999)),
        "messages": [{"role": "user", "content": "hello"}],
    }
    headers = {"Authorization": auth, "Content-Type": "application/json"}
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        logger.debug("bad_unknown_model", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_unknown_model_exc", error=str(exc))


async def _bad_empty_messages(auth: str):
    payload = {"model": random.choice(CHAT_MODELS), "messages": []}
    headers = {"Authorization": auth, "Content-Type": "application/json"}
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        logger.debug("bad_empty_messages", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_empty_messages_exc", error=str(exc))


async def _bad_revoked_key():
    headers = {
        "Authorization": "Bearer revoked-key-001:bad-secret",
        "Content-Type": "application/json",
    }
    payload = {
        "model": random.choice(CHAT_MODELS),
        "messages": [{"role": "user", "content": "hello"}],
    }
    try:
        resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
        logger.debug("bad_revoked_key", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_revoked_key_exc", error=str(exc))


async def _bad_missing_auth():
    payload = {
        "model": random.choice(CHAT_MODELS),
        "messages": [{"role": "user", "content": "hello"}],
    }
    try:
        resp = await fire_request("POST", "/v1/chat/completions", {}, payload)
        logger.debug("bad_missing_auth", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_missing_auth_exc", error=str(exc))


async def _bad_content_type(auth: str):
    headers = {"Authorization": auth, "Content-Type": "text/plain"}
    client = __import__("utils", fromlist=["get_client"]).get_client()
    url = f"{__import__('config').GATEWAY}/v1/chat/completions"
    try:
        resp = await client.post(url, headers=headers, content=b"plain text body")
        logger.debug("bad_content_type", status=resp.status_code)
    except Exception as exc:
        logger.warning("bad_content_type_exc", error=str(exc))


async def bad_worker():
    target_qps = SCENARIO_QPS["bad"]
    while True:
        try:
            key = key_pool.get("bad")
            auth = key_pool.auth_header(key)
            case = random.choice(BAD_CASES)

            if case == "invalid_json":
                await _bad_invalid_json(auth)
            elif case == "oversized_tokens":
                await _bad_oversized_tokens(auth)
            elif case == "unknown_model":
                await _bad_unknown_model(auth)
            elif case == "empty_messages":
                await _bad_empty_messages(auth)
            elif case == "revoked_key":
                await _bad_revoked_key()
            elif case == "missing_auth":
                await _bad_missing_auth()
            elif case == "bad_content_type":
                await _bad_content_type(auth)

        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.exception("bad_worker_exception", error=str(exc))

        await asyncio.sleep(sleep_for_qps(target_qps))
