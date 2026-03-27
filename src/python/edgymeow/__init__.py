try:
    from ._version import __version__
except ImportError:
    from importlib.metadata import version
    __version__ = version("edgymeow")

from .client import WhatsAppRPCClient

__all__ = ["WhatsAppRPCClient"]
