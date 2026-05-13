"""Shared utilities for loadgen scenarios."""
import asyncio
import random
import sys
import time
from typing import Any, Callable, Dict, Optional

import httpx
import structlog

from config import GATEWAY

logger = structlog.get_logger("loadgen.utils")

# Shared async HTTP client with connection pooling.
_client: Optional[httpx.AsyncClient] = None


def get_client() -> httpx.AsyncClient:
    global _client
    if _client is None or _client.is_closed:
        _client = httpx.AsyncClient(
            http2=True,
            limits=httpx.Limits(max_connections=1000, max_keepalive_connections=200),
            timeout=httpx.Timeout(60.0, connect=5.0, read=60.0, write=10.0),
        )
    return _client


async def close_client():
    global _client
    if _client and not _client.is_closed:
        await _client.aclose()
        _client = None


def random_prompt(prompts: list) -> str:
    return random.choice(prompts)


def random_model(models: list) -> str:
    return random.choice(models)


def pick_weighted(models: list, weights: Optional[list] = None) -> str:
    if weights and len(weights) == len(models):
        return random.choices(models, weights=weights, k=1)[0]
    return random.choice(models)


def jitter(base: float, ratio: float = 0.2) -> float:
    """Add +/- ratio jitter to base interval."""
    return base * (1 + random.uniform(-ratio, ratio))


def sleep_for_qps(target_qps: float) -> float:
    """Return sleep duration to achieve target_qps from a single worker."""
    if target_qps <= 0:
        return 1.0
    return jitter(1.0 / target_qps)


async def fire_request(
    method: str,
    path: str,
    headers: Dict[str, str],
    payload: Optional[Dict] = None,
    stream: bool = False,
) -> httpx.Response:
    """Fire a single HTTP request through the shared client."""
    client = get_client()
    url = f"{GATEWAY}{path}"
    start = time.monotonic()
    try:
        if stream:
            # For streaming we use stream=True and consume chunks.
            response = await client.request(
                method, url, headers=headers, json=payload
            )
        else:
            response = await client.request(
                method, url, headers=headers, json=payload
            )
        latency = time.monotonic() - start
        logger.debug(
            "request_done",
            method=method,
            path=path,
            status=response.status_code,
            latency_ms=round(latency * 1000, 2),
        )
        return response
    except httpx.HTTPError as exc:
        latency = time.monotonic() - start
        logger.warning(
            "request_failed",
            method=method,
            path=path,
            error=str(exc),
            latency_ms=round(latency * 1000, 2),
        )
        raise


def setup_logging():
    import logging
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=logging.INFO,
    )
    structlog.configure(
        processors=[
            structlog.stdlib.filter_by_level,
            structlog.stdlib.add_logger_name,
            structlog.stdlib.add_log_level,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        context_class=dict,
        logger_factory=structlog.stdlib.LoggerFactory(),
        wrapper_class=structlog.stdlib.BoundLogger,
        cache_logger_on_first_use=True,
    )


async def graceful_shutdown(tasks: list, signal_name: str = "SIGTERM"):
    logger.info("shutdown_initiated", signal=signal_name)
    for t in tasks:
        t.cancel()
    await asyncio.gather(*tasks, return_exceptions=True)
    await close_client()
    logger.info("shutdown_complete")
    sys.exit(0)
