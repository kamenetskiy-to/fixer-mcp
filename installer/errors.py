"""Exception classes for Fixer MCP installer and update engine."""


class InstallerError(Exception):
    """Base exception for all installer and updater errors."""
    pass


class ConcurrentInstallError(InstallerError):
    """Raised when another installer or updater process holds the install lock."""
    pass


class VerificationError(InstallerError):
    """Raised when descriptor, checksum, or payload archive verification fails."""
    pass


class CandidateBootstrapError(InstallerError):
    """Raised when candidate binary schema bootstrap check fails."""
    pass


class ActiveRuntimeError(InstallerError):
    """Raised when active Fixer processes prevent immediate activation."""
    pass


class DevelopmentCheckoutError(InstallerError):
    """Raised when auto-update is attempted on a development checkout."""
    pass


class DescriptorFetchError(InstallerError):
    """Raised when release descriptor cannot be fetched or parsed."""
    pass


class PayloadFetchError(InstallerError):
    """Raised when release payload archive cannot be downloaded or read."""
    pass
