"""Loadgen orchestrator: start all scenario workers and manage lifecycle."""
import asyncio
import os
import random
import signal
import sys

import structlog

from config import GATEWAY
from scenario_agent import agent_worker
from scenario_bad import bad_worker
from scenario_chat import chat_worker
from scenario_embedding import embedding_worker
from scenario_session import session_worker
from scenario_spike import spike_worker
from utils import close_client, graceful_shutdown, setup_logging

logger = structlog.get_logger("loadgen.main")

SCENARIO_WORKERS = [
    ("chat", chat_worker),
    ("session", session_worker),
    ("agent", agent_worker),
    ("embedding", embedding_worker),
    ("bad", bad_worker),
    ("spike", spike_worker),
]

# Number of parallel workers per scenario. Total QPS = scenario_qps * workers.
WORKERS_PER_SCENARIO = int(os.getenv("LOADGEN_WORKERS", "3"))


async def main():
    setup_logging()
    logger.info(
        "loadgen_starting",
        gateway=GATEWAY,
        scenarios=[n for n, _ in SCENARIO_WORKERS],
        workers_per_scenario=WORKERS_PER_SCENARIO,
    )

    tasks = []
    for name, factory in SCENARIO_WORKERS:
        # Stagger start to avoid thundering-herd against rate limiter.
        await asyncio.sleep(random.uniform(0.5, 2.5))
        for i in range(WORKERS_PER_SCENARIO):
            t = asyncio.create_task(factory(), name=f"worker-{name}-{i}")
            tasks.append(t)
        logger.info("workers_started", name=name, count=WORKERS_PER_SCENARIO)

    loop = asyncio.get_running_loop()

    def _on_signal(sig):
        logger.info("signal_received", signal=sig.name)
        asyncio.create_task(graceful_shutdown(tasks, sig.name))

    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, lambda s=sig: _on_signal(s))
        except NotImplementedError:
            # Windows does not support add_signal_handler for SIGTERM in some cases.
            pass

    # Wait for all workers (they run forever until cancelled).
    try:
        await asyncio.gather(*tasks, return_exceptions=True)
    except asyncio.CancelledError:
        pass
    finally:
        await close_client()
        logger.info("loadgen_stopped")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logger.info("keyboard_interrupt")
        sys.exit(0)
