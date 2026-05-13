"""LangChain Agent scenario: tool-use + multi-turn reasoning."""
import asyncio
import random

import structlog

from config import SCENARIO_QPS
from keys import key_pool
from utils import fire_request

logger = structlog.get_logger("loadgen.agent")

AGENT_QUESTIONS = [
    "What is the weather in Tokyo right now?",
    "Calculate 127 * 43 + 89",
    "Search for the latest news about AI safety.",
    "Convert 100 USD to EUR using today's rate.",
    "Find the population of Canada and compare it to Japan.",
    "What is the square root of 98765?",
    "List the top 3 programming languages in 2024.",
]

# Minimal tool schema to trigger tool-use path in gateway.
TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "get_weather",
            "description": "Get current weather for a city",
            "parameters": {
                "type": "object",
                "properties": {
                    "city": {"type": "string", "description": "City name"},
                },
                "required": ["city"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "calculator",
            "description": "Evaluate a math expression",
            "parameters": {
                "type": "object",
                "properties": {
                    "expression": {"type": "string", "description": "Math expression"},
                },
                "required": ["expression"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "web_search",
            "description": "Search the web for a query",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search query"},
                },
                "required": ["query"],
            },
        },
    },
]


def _fake_tool_result(name: str, args: dict) -> str:
    """Return a fake tool result so the agent can complete its turn."""
    if name == "get_weather":
        city = args.get("city", "unknown")
        return f"Weather in {city}: 22°C, sunny."
    if name == "calculator":
        expr = args.get("expression", "0")
        try:
            # Safe eval for simple math only.
            result = eval(expr, {"__builtins__": {}}, {})
            return f"Result: {result}"
        except Exception:
            return "Error: invalid expression"
    if name == "web_search":
        query = args.get("query", "unknown")
        return f"Search results for '{query}': 1) AI safety report 2024 2) New EU AI Act guidelines."
    return "Tool executed."


async def agent_worker():
    target_qps = SCENARIO_QPS["agent"]
    while True:
        try:
            key = key_pool.get("agent")
            auth = key_pool.auth_header(key)
            model = "LongCat-Flash-Chat"
            question = random.choice(AGENT_QUESTIONS)
            messages = [{"role": "user", "content": question}]
            max_turns = 5

            for turn in range(max_turns):
                payload = {
                    "model": model,
                    "messages": messages,
                    "tools": TOOLS,
                    "tool_choice": "auto",
                    "stream": False,
                    "max_tokens": 300,
                }
                headers = {
                    "Authorization": auth,
                    "Content-Type": "application/json",
                }
                try:
                    resp = await fire_request("POST", "/v1/chat/completions", headers, payload)
                    if resp.status_code != 200:
                        logger.debug("agent_turn_non_200", turn=turn, status=resp.status_code)
                        break

                    body = resp.json()
                    choice = body["choices"][0]
                    message = choice["message"]
                    messages.append(message)

                    tool_calls = message.get("tool_calls", [])
                    if not tool_calls:
                        logger.info("agent_done", turns=turn + 1, question=question)
                        break

                    # Fake execute each tool call.
                    for tc in tool_calls:
                        fn = tc["function"]
                        result = _fake_tool_result(fn["name"], eval(fn["arguments"]))
                        messages.append({
                            "tool_call_id": tc["id"],
                            "role": "tool",
                            "name": fn["name"],
                            "content": result,
                        })

                    # Small delay between agent reasoning steps.
                    await asyncio.sleep(random.uniform(0.5, 1.5))

                except Exception as exc:
                    logger.warning("agent_turn_exc", turn=turn, error=str(exc))
                    break

        except asyncio.CancelledError:
            raise
        except Exception as exc:
            logger.exception("agent_worker_exception", error=str(exc))

        # Agent is slow; sleep longer between full runs.
        await asyncio.sleep(1.0 / target_qps if target_qps > 0 else 10.0)
