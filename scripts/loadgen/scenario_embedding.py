"""Embedding / batch scenario."""
import asyncio
import random

import structlog

from config import EMBEDDING_BATCH_SIZE, EMBEDDING_TEXTS, SCENARIO_QPS
from keys import key_pool
from utils import fire_request, sleep_for_qps

logger = structlog.get_logger("loadgen.embedding")


async def embedding_worker():
    target_qps = SCENARIO_QPS["embedding"]
    while True:
        try:
            key = key_pool.get("embedding")
            auth = key_pool.auth_header(key)
            batch_size = random.randint(*EMBEDDING_BATCH_SIZE)
            texts = [random.choice(EMBEDDING_TEXTS) for _ in range(batch_size)]

            payload = {
                "model": "text-embedding-3-small",
                "input": texts,
            }
            headers = {
                "Authorization": auth,
                "Content-Type": "application/json",
            }
            try:
                resp = await fire_request("POST", "/v1/embeddings", headers, payload)
                if resp.status_code == 200:
                    body = resp.json()
                    data = body.get("data", [])
                    logger.debug(
                        "embedding_ok",
                        batch_size=batch_size,
                        returned=len(data),
                    )
                else:
                    logger.debug(
                        "embedding_non_200",
                        batch_size=batch_size,
                        status=resp.status_code,
                        body=resp.text[:200],
                    )
            except Exception as exc:
                logger.warning("embedding_error", batch_size=batch_size, error=str(exc))
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.exception("embedding_worker_exception", error=str(exc))

        await asyncio.sleep(sleep_for_qps(target_qps))
