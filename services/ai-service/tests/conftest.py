"""Shared fixtures: a real server running the real handlers against a scripted
provider, so the suite exercises the gRPC path without a network call.
"""

import grpc
import pytest

from ai_service.config import Settings
from ai_service.providers import FakeProvider


@pytest.fixture(autouse=True)
def never_call_a_vendor(monkeypatch):
    """No test may construct a real provider.

    An early version of the fixture below passed a dummy API key with no
    scripted provider, which built a live client and sent a real request. The
    suite must be incapable of that, not merely careful about it.
    """
    import ai_service.providers as providers

    def forbidden(*_args, **_kwargs):
        raise AssertionError("a test tried to construct a real provider; script a FakeProvider")

    monkeypatch.setattr(providers, "AnthropicProvider", forbidden)
    monkeypatch.setattr(providers, "OpenAIProvider", forbidden)


@pytest.fixture
def make_stub():
    """Start a server backed by ``answers`` and return ``(stub, provider)``.

    With neither ``answers`` nor ``provider``, the server runs with no
    provider at all — the unconfigured-credential case.
    """
    running = []

    def _make(answers=None, provider=None, **overrides):
        from ai_service.gen.hireassist.ai.v1 import ai_service_pb2_grpc as pb_grpc
        from ai_service.server import build_server

        if provider is None and answers is not None:
            provider = FakeProvider(answers)

        # No key when there is no provider, so provider_for cannot build one.
        config = Settings(port=0, api_key="test-key" if provider else None, **overrides)
        server, port = build_server(config, provider=provider)
        server.start()
        channel = grpc.insecure_channel(f"localhost:{port}")
        running.append((server, channel))
        return pb_grpc.AiServiceStub(channel), provider

    yield _make

    for server, channel in running:
        channel.close()
        server.stop(None)
