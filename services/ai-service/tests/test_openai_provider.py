"""The OpenAI adapter against a scripted client, and the provider defaults.

No test builds a real OpenAI client: the adapter takes the client it is given,
and conftest forbids provider_for from constructing one.
"""

from types import SimpleNamespace

import pytest

from ai_service.config import Settings
from ai_service.provider import ProviderError
from ai_service.providers.openai_provider import OpenAIProvider


class ScriptedClient:
    """Answers chat.completions.create with a fixed response and keeps the request."""

    def __init__(self, response=None, error=None):
        self.request = None
        self._response, self._error = response, error
        self.chat = SimpleNamespace(completions=SimpleNamespace(create=self._create))

    def _create(self, **request):
        self.request = request
        if self._error:
            raise self._error
        return self._response


def response(arguments, name="scoring", finish_reason="tool_calls"):
    call = SimpleNamespace(function=SimpleNamespace(name=name, arguments=arguments))
    return SimpleNamespace(
        model="gpt-4.1-mini-2025-04-14",
        choices=[
            SimpleNamespace(message=SimpleNamespace(tool_calls=[call]), finish_reason=finish_reason)
        ],
        usage=SimpleNamespace(prompt_tokens=120, completion_tokens=30),
    )


def ask(provider):
    return provider.json_answer(
        system="sys",
        user="usr",
        schema={"type": "object", "properties": {"score": {"type": "integer"}}},
        schema_name="scoring",
        schema_description="a score",
    )


def openai_settings(**overrides):
    return Settings(provider="openai", api_key="test-key", **overrides)


def test_the_answer_is_the_forced_function_call():
    client = ScriptedClient(response('{"score": 78}'))
    answer = ask(OpenAIProvider(openai_settings(), client=client))

    assert answer.data == {"score": 78}
    assert answer.model == "gpt-4.1-mini-2025-04-14"  # what answered, not what was asked for
    assert (answer.input_tokens, answer.output_tokens) == (120, 30)
    assert client.request["tool_choice"] == {"type": "function", "function": {"name": "scoring"}}
    assert client.request["messages"][0] == {"role": "system", "content": "sys"}


def test_no_function_call_is_a_provider_error():
    reply = response("{}", name="something-else", finish_reason="stop")
    with pytest.raises(ProviderError, match="finish_reason=stop"):
        ask(OpenAIProvider(openai_settings(), client=ScriptedClient(reply)))


def test_malformed_arguments_are_a_provider_error():
    with pytest.raises(ProviderError, match="malformed"):
        ask(OpenAIProvider(openai_settings(), client=ScriptedClient(response('{"score": '))))


def test_a_vendor_exception_does_not_escape():
    class RateLimited(Exception):
        pass

    with pytest.raises(ProviderError, match="RateLimited"):
        ask(
            OpenAIProvider(openai_settings(), client=ScriptedClient(error=RateLimited("slow down")))
        )


def test_each_provider_has_its_own_default_model():
    assert openai_settings().model == "gpt-4.1-mini"
    assert Settings(provider="anthropic").model == "claude-haiku-4-5-20251001"
    assert openai_settings(model="gpt-4o").model == "gpt-4o"


def test_an_empty_provider_means_the_default():
    # A compose file passes an unset variable through as an empty string.
    assert Settings(provider="", model="").provider == "anthropic"
    assert Settings(provider="OpenAI").provider == "openai"
