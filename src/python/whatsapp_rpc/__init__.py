from importlib.metadata import version

from .client import WhatsAppRPCClient

__version__ = version("whatsapp-rpc")
__all__ = ["WhatsAppRPCClient"]
