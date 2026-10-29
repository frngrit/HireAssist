"""Provider adapters. One per vendor, all satisfying ai_service.provider.Provider."""

from ai_service.providers.anthropic_provider import AnthropicProvider
from ai_service.providers.canned import CannedProvider
from ai_service.providers.fake import FakeProvider
from ai_service.providers.openai_provider import OpenAIProvider

__all__ = ["AnthropicProvider", "CannedProvider", "FakeProvider", "OpenAIProvider", "provider_for"]


def provider_for(settings):
    """The provider the configuration asks for, or None when no credential is set.

    Returning None rather than raising lets the service start without a key —
    it comes up, reports itself, and fails the calls that actually need the
    model, which is easier to diagnose than a container that will not boot.
    """
    # The demo provider needs no credential — that is its whole point.
    if settings.provider == "canned":
        return CannedProvider(settings)
    if not settings.api_key:
        return None
    if settings.provider == "anthropic":
        return AnthropicProvider(settings)
    if settings.provider == "openai":
        return OpenAIProvider(settings)
    raise ValueError(
        f"unknown provider {settings.provider!r}: "
        "set AI_SERVICE_PROVIDER to anthropic, openai or canned"
    )
