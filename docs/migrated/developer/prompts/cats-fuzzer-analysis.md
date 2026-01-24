# Prompt to analyze CATS fuzzer results

<!-- Migrated from: docs/developer/prompts/cats-fuzzer-analysis.md on 2025-01-24 -->

I want you to analyze the test results from the CATS fuzzer that I just ran. I want you to use sub-agents to analyze results of each test independently, and then prepare a consolidated report for me. Run as many sub-agents at a time as is reasonable because there are a large number of test results. Each test result is recorded in a separate JSON file (file name like `Test*.json`) in `test/outputs/cats/report/` and indicates the test result status (`error`, `warn`, `success`). I am not interested in success results; you should skip them.

Your sub-agent will be a security penetration tester who is also an experienced backend server API developer. The job of the sub-agent will be to analyze a single fuzz test result, and decide whether the result is valid or not, and if so, come up with a simple recommendation what to do about it. The recommendation should be "should fix" or "should ignore" or "false positive" or, if the sub-agent is not certain, then "should investigate", and in all cases a reason why in a few words. And the sub-agent should recommend a priority level (high, medium, low) for the fix based on the security impact.

An example of a false positive would be in Test 6, where CATS indicates that the server returned 200 for GET / without an authorization header, but the fuzzer expected a 4xx error due to the unauthenticated request. However the / endpoint is specifically designed to be unauthenticated and should have a declaration of `security: []` on the endpoint (it is a bug if a public API is missing that declaration).

An example of a "should investigate" would be Test 1, because nothing in our design indicates that we should or should not support an "Accept-Language" header.

An example of a "should fix" with low priority would be Test 8, where the fuzzer sent an undocumented (and unsupported) HTTP method, and the server responded with a 400 error (bad request) instead of 405 (method not allowed).

An example of a "should fix" with a medium priority would be Test 25, where the response content type didn't match what was declared in the schema.

After the sub-agents have analyzed all the results, I want you to present me a prioritized list of what to fix, and what to ignore, and why. Group things together as much as possible so that I don't have to read tens of thousands of lines.

---

## Verification Summary

- **File path verified**: `test/outputs/cats/report/` (referenced in Makefile and scripts)
- **File pattern verified**: `Test*.json` (used by `scripts/parse-cats-results.py`)
- **Status values verified**: `error`, `warn`, `success` (per `scripts/parse-cats-results.py`)
- **CATS tool verified**: [Endava/cats](https://github.com/Endava/cats) - REST API Fuzzer for OpenAPI endpoints
  - Install: `brew tap endava/tap && brew install cats`
  - Documentation: https://endava.github.io/cats/
- **Test examples**: Illustrative examples; actual test numbers vary per run
