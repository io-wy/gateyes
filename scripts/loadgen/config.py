"""Loadgen configuration."""
import os

GATEWAY = os.getenv("GATEWAY_URL", "http://localhost:8028")

# Key pool: each scenario has its own key with independent quota/qps.
# Must match the apiKeys section in gateway configs/config.yaml.
KEYS = [
    {
        "name": "chat-user",
        "key": "chat-key-001",
        "secret": "chat-secret-001",
        "quota": 5_000_000,
        "qps": 200,
        "models": [],
    },
    {
        "name": "session-user",
        "key": "session-key-001",
        "secret": "session-secret-001",
        "quota": 3_000_000,
        "qps": 100,
        "models": [],
    },
    {
        "name": "agent-user",
        "key": "agent-key-001",
        "secret": "agent-secret-001",
        "quota": 2_000_000,
        "qps": 50,
        "models": [],
    },
    {
        "name": "embedding-user",
        "key": "embed-key-001",
        "secret": "embed-secret-001",
        "quota": 1_000_000,
        "qps": 100,
        "models": [],
    },
    {
        "name": "bad-user",
        "key": "bad-key-001",
        "secret": "bad-secret-001",
        "quota": 1_000_000,
        "qps": 50,
        "models": [],
    },
    {
        "name": "spike-user",
        "key": "spike-key-001",
        "secret": "spike-secret-001",
        "quota": 10_000_000,
        "qps": 500,
        "models": [],
    },
    # Fallback generic key (infinite quota, shared by demo traffic).
    {
        "name": "demo-user",
        "key": "demo-key-001",
        "secret": "demo-secret-001",
        "quota": -1,
        "qps": 1000,
        "models": [],
    },
]

# Model pool for chat scenarios.
CHAT_MODELS = [
    "LongCat-Flash-Chat",
    "glm-5.1",
    "kimi-for-coding",
    "gpt-5.1",
]

# Prompt pool for variety.
PROMPTS = [
    "Explain quantum computing in one sentence.",
    "Write a Python function to reverse a string.",
    "What are the pros and cons of microservices?",
    "Summarize the theory of relativity.",
    "Give me a 3-day meal plan for a keto diet.",
    "Translate 'Hello world' to Japanese, French and German.",
    "Debug this code: for i in range(10): print(i++",
    "Recommend a book about distributed systems.",
    "How does a blockchain reach consensus?",
    "Write a bash script that backs up a directory to S3.",
]

# Scenario QPS targets.
SCENARIO_QPS = {
    "chat": 20.0,
    "session": 3.0,
    "agent": 1.0,
    "embedding": 2.0,
    "bad": 1.0,
    "spike": None,  # handled separately
}

# Spike config.
SPIKE_INTERVAL_SECONDS = 300  # every 5 min
SPIKE_DURATION_SECONDS = 20
SPIKE_QPS = 100

# Session config.
SESSION_CONVERSATION_LENGTH = (3, 6)  # min, max turns
SESSION_INTERVAL_SECONDS = 10

# Embedding config.
EMBEDDING_BATCH_SIZE = (1, 8)
EMBEDDING_TEXTS = [
    "Machine learning is a subset of artificial intelligence.",
    "Kubernetes is a container orchestration platform.",
    "The quick brown fox jumps over the lazy dog.",
    "Docker containers are lightweight and portable.",
    "REST APIs use HTTP methods like GET, POST, PUT, DELETE.",
    "Graph databases store data in nodes and edges.",
    "CI/CD pipelines automate build, test and deployment.",
    "Observability includes metrics, logs and traces.",
]
