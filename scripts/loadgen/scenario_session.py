"""Session stickiness scenario: same session_id, multi-turn conversation."""
import asyncio
import random
import uuid

import structlog

from config import CHAT_MODELS, PROMPTS, SCENARIO_QPS, SESSION_CONVERSATION_LENGTH, SESSION_INTERVAL_SECONDS
from keys import key_pool
from utils import fire_request, sleep_for_qps

logger = structlog.get_logger("loadgen.session")

SESSION_PROMPTS = [
    "What is Kubernetes?",
    "Can you explain pods vs containers?",
    "Show me a simple deployment yaml.",
    "How do services work in K8s?",
    "What is an ingress controller?",
    "Explain helm charts briefly.",
]


async def session_worker():
    target_qps = SCENARIO_QPS["session"]
    while True:
        try:
            key = key_pool.get("session")
            auth = key_pool.auth_header(key)
            model = random.choice(CHAT_MODELS)
            session_id = str(uuid.uuid4())[:8]
            turns = random.randint(*SESSION_CONVERSATION_LENGTH)
            messages = []

            for turn in range(turns):
                prompt = SESSION_PROMPTS[turn % len(SESSION_PROMPTS)]
                messages.append({"role": "user", "content": prompt})
                payload = {
                    "model": model,
                    "messages": messages,
                    "stream": random.random() < 0.5,
                    "max_tokens": random.randint(30, 120),
                    "session_id": session_id,
                }
                headers = {
                    "Authorization": auth,
                    "Content-Type": "application/json",
                }
                try:
                    resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
                    if resp.status_code == 200:
                        body = resp.json()
                        reply = body["choices"][0]["message"]["content"]
                        messages.append({"role": "assistant", "content": reply})
                        logger.debug(
                            "session_turn",
                            session_id=session_id,
                            turn=turn + 1,
                            model=model,
                            provider=body.get("model", "unknown"),
                        )
                    else:
                        logger.debug(
                            "session_turn_error",
                            session_id=session_id,
                            turn=turn + 1,
                            status=resp.status_code,
                        )
                        break
                except Exception as exc:
                    logger.warning("session_turn_exc", session_id=session_id, turn=turn + 1, error=str(exc))
                    break

                # Small pause between turns to feel like a real conversation.
                await asyncio.sleep(random.uniform(1.0, 3.0))

            logger.info("session_complete", session_id=session_id, turns=turns, model=model)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.exception("session_worker_exception", error=str(exc))

        await asyncio.sleep(sleep_for_qps(target_qps))
