/**
 * Models the TMI slogging package as providing sanitization for log injection attacks.
 *
 * The slogging package (github.com/ericfitz/tmi/internal/slogging) provides structured
 * logging with built-in sanitization via SanitizeLogMessage(). This function removes
 * newlines, carriage returns, and tabs that could be used for log injection attacks (CWE-117).
 *
 * All logging methods in the package call SanitizeLogMessage() before writing to the log:
 *   - Logger.Debug/Info/Warn/Error
 *   - Logger.DebugCtx/InfoCtx/WarnCtx/ErrorCtx
 *   - ContextLogger.Debug/Info/Warn/Error
 *   - ContextLogger.DebugCtx/InfoCtx/WarnCtx/ErrorCtx
 *   - FallbackLogger.Debug/Info/Warn/Error
 *
 * This library teaches CodeQL that:
 *   1. SanitizeLogMessage() is a sanitizer that breaks taint flow for log injection
 *   2. The logging methods in slogging are safe sinks because they sanitize internally
 */

import go
import semmle.go.dataflow.DataFlow
import semmle.go.security.LogInjection

/**
 * The slogging package path.
 */
private string sloggingPackage() { result = "github.com/ericfitz/tmi/internal/slogging" }

/**
 * A call to SanitizeLogMessage in the slogging package.
 * This function sanitizes log messages by removing control characters.
 */
class SanitizeLogMessageCall extends DataFlow::CallNode {
  SanitizeLogMessageCall() {
    this.getTarget().hasQualifiedName(sloggingPackage(), "SanitizeLogMessage")
  }

  /** Gets the input argument being sanitized. */
  DataFlow::Node getInput() { result = this.getArgument(0) }

  /** Gets the sanitized output. */
  DataFlow::Node getOutput() { result = this.getResult() }
}

/**
 * Models SanitizeLogMessage as a sanitizer for log injection.
 * Data flowing through this function is considered sanitized.
 */
class SloggingSanitizer extends LogInjection::Sanitizer {
  SloggingSanitizer() {
    exists(SanitizeLogMessageCall call | this = call.getOutput())
  }
}

/**
 * Models calls to slogging Logger methods as safe sinks.
 * These methods internally call SanitizeLogMessage before logging.
 */
class SloggingLoggerMethod extends DataFlow::CallNode {
  string methodName;

  SloggingLoggerMethod() {
    exists(Method m |
      m.hasQualifiedName(sloggingPackage(), "Logger", methodName) and
      methodName in ["Debug", "Info", "Warn", "Error", "DebugCtx", "InfoCtx", "WarnCtx", "ErrorCtx"] and
      this = m.getACall()
    )
  }

  /** Gets the format/message argument. */
  DataFlow::Node getMessageArg() {
    // First argument is always the format string or message
    result = this.getArgument(0)
    or
    // For *Ctx methods, first arg is context, second is message
    methodName.matches("%Ctx") and result = this.getArgument(1)
  }
}

/**
 * Models calls to slogging ContextLogger methods as safe sinks.
 * These methods internally call SanitizeLogMessage before logging.
 */
class SloggingContextLoggerMethod extends DataFlow::CallNode {
  string methodName;

  SloggingContextLoggerMethod() {
    exists(Method m |
      m.hasQualifiedName(sloggingPackage(), "ContextLogger", methodName) and
      methodName in ["Debug", "Info", "Warn", "Error", "DebugCtx", "InfoCtx", "WarnCtx", "ErrorCtx"] and
      this = m.getACall()
    )
  }

  /** Gets the format/message argument. */
  DataFlow::Node getMessageArg() {
    result = this.getArgument(0)
  }
}

/**
 * Models calls to slogging FallbackLogger methods as safe sinks.
 * These methods internally call SanitizeLogMessage before logging.
 */
class SloggingFallbackLoggerMethod extends DataFlow::CallNode {
  string methodName;

  SloggingFallbackLoggerMethod() {
    exists(Method m |
      m.hasQualifiedName(sloggingPackage(), "FallbackLogger", methodName) and
      methodName in ["Debug", "Info", "Warn", "Error"] and
      this = m.getACall()
    )
  }

  /** Gets the format/message argument. */
  DataFlow::Node getMessageArg() {
    result = this.getArgument(0)
  }
}

/**
 * Extends the LogInjection sanitizer to recognize data flowing into slogging methods.
 * Since all slogging methods call SanitizeLogMessage internally, any data that reaches
 * a slogging method is effectively sanitized before being logged.
 */
class SloggingMethodSanitizer extends LogInjection::Sanitizer {
  SloggingMethodSanitizer() {
    this = any(SloggingLoggerMethod m).getMessageArg()
    or
    this = any(SloggingContextLoggerMethod m).getMessageArg()
    or
    this = any(SloggingFallbackLoggerMethod m).getMessageArg()
  }
}

/**
 * Models the SimpleLogger interface methods as safe sinks.
 * The SimpleLogger interface is implemented by Logger, ContextLogger, and FallbackLogger,
 * all of which sanitize log messages.
 */
class SloggingSimpleLoggerCall extends DataFlow::CallNode {
  SloggingSimpleLoggerCall() {
    exists(Method m |
      // Match any method call on a type that implements SimpleLogger
      // where the receiver type is from the slogging package
      m.getReceiverType().getUnderlyingType().(PointerType).getBaseType().hasQualifiedName(sloggingPackage(), _) and
      m.getName() in ["Debug", "Info", "Warn", "Error"] and
      this = m.getACall()
    )
  }
}
