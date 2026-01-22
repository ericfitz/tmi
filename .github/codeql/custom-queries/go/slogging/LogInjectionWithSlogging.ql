/**
 * @name Log injection (with slogging sanitizer awareness)
 * @description Building log entries from user-controlled data may allow
 *              attackers to forge log entries, obscure security events,
 *              or corrupt log files. This query is aware of the TMI slogging
 *              package's built-in sanitization.
 * @kind path-problem
 * @problem.severity warning
 * @security-severity 7.8
 * @precision high
 * @id go/log-injection-slogging-aware
 * @tags security
 *       external/cwe/cwe-117
 */

import go
import semmle.go.security.LogInjection
import SloggingSanitizers
import LogInjection::Flow::PathGraph

from LogInjection::Flow::PathNode source, LogInjection::Flow::PathNode sink
where LogInjection::Flow::flowPath(source, sink)
select sink.getNode(), source, sink, "Log entry depends on a $@.", source.getNode(),
  "user-provided value"
