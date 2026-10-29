"""OpenAI adapter — the substitute provider ADR-004 names beside Anthropic.

Like the Anthropic adapter, structured output comes from forcing a function
call rather than from asking for JSON in the prompt, so the API holds the
answer to the schema. Strict mode is not used: it needs every property
required and no open objects, which the prompts' schemas do not promise, and
service.py validates every answer against its contract anyway.
"""

import json
import logging
from typing import Any

from ai_service.config import Settings
from ai_service.provider import ProviderAnswer, ProviderError

log = logging.getLogger(__name__)


class OpenAIProvider:
    def __init__(self, settings: Settings, client: Any = None) -> None:
        if not settings.api_key:
            raise ProviderError("no API key configured")
        if client is None:
            import openai

            client = openai.OpenAI(
                api_key=settings.api_key,
                timeout=settings.request_timeout_s,
                max_retries=0,  # the caller retries with backoff (ADR-002); not twice.
            )
        self._client = client
        self._model = settings.model
        self._max_tokens = settings.max_tokens

    def json_answer(
        self,
        *,
        system: str,
        user: str,
        schema: dict[str, Any],
        schema_name: str,
        schema_description: str,
    ) -> ProviderAnswer:
        try:
            response = self._client.chat.completions.create(
                model=self._model,
                max_completion_tokens=self._max_tokens,
                messages=[
                    {"role": "system", "content": system},
                    {"role": "user", "content": user},
                ],
                tools=[
                    {
                        "type": "function",
                        "function": {
                            "name": schema_name,
                            "description": schema_description,
                            "parameters": schema,
                        },
                    }
                ],
                tool_choice={"type": "function", "function": {"name": schema_name}},
            )
        except Exception as exc:
            # Wrapped deliberately: an openai type in a handler's except clause
            # would make every caller depend on the vendor.
            raise ProviderError(f"provider call failed: {type(exc).__name__}: {exc}") from exc

        choice = response.choices[0] if response.choices else None
        for call in (choice.message.tool_calls or []) if choice else []:
            if call.function.name != schema_name:
                continue
            try:
                data = json.loads(call.function.arguments)
            except json.JSONDecodeError as exc:
                raise ProviderError(f"provider returned malformed {schema_name} arguments") from exc
            usage = response.usage
            return ProviderAnswer(
                data=data,
                model=response.model,
                input_tokens=getattr(usage, "prompt_tokens", 0) or 0,
                output_tokens=getattr(usage, "completion_tokens", 0) or 0,
            )

        reason = choice.finish_reason if choice else "no choices"
        raise ProviderError(f"provider returned no {schema_name} answer (finish_reason={reason})")
