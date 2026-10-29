"""A scripted provider, so the suite exercises the real handlers without a
network call, an API key, or a bill.

Tests assert on what the handler does with an answer; what the model would
have said is the test's input, not the thing under test.
"""

from typing import Any

from ai_service.provider import ProviderAnswer, ProviderError


class FakeProvider:
    def __init__(self, answers: dict[str, Any], model: str = "fake-model-1") -> None:
        """``answers`` maps a schema name to the answer for it: a dict to
        return, an exception to raise, or a callable taking the user message.
        """
        self._answers = answers
        self._model = model
        self.calls: list[dict[str, Any]] = []

    def json_answer(
        self,
        *,
        system: str,
        user: str,
        schema: dict[str, Any],
        schema_name: str,
        schema_description: str,
    ) -> ProviderAnswer:
        self.calls.append(
            {"schema_name": schema_name, "system": system, "user": user, "schema": schema}
        )

        if schema_name not in self._answers:
            raise ProviderError(f"fake has no answer scripted for {schema_name}")

        answer = self._answers[schema_name]
        if isinstance(answer, Exception):
            raise answer
        if callable(answer):
            answer = answer(user)
        return ProviderAnswer(data=answer, model=self._model)
