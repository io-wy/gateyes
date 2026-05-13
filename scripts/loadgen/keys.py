"""API key management for loadgen."""
import random
from typing import Dict, List

from config import KEYS


class KeyPool:
    def __init__(self):
        self._keys = KEYS

    def get(self, role: str) -> Dict:
        """Return a key tagged with the given role."""
        candidates = [k for k in self._keys if k["name"].startswith(role)]
        if not candidates:
            # fallback to generic chat key
            candidates = [k for k in self._keys if k["name"] == "chat-user"]
        return random.choice(candidates)

    def auth_header(self, key: Dict) -> str:
        return f"Bearer {key['key']}:{key['secret']}"


key_pool = KeyPool()
