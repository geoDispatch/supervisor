"CAMARA_TIMEOUT"  → Nokia NaC didn't respond in time
                  → Phone is empty (not device-specific)
                  → Fatal: false (pipeline continues with cached data)

"AGENT_ERROR"     → Python AI agent crashed or returned invalid JSON
                  → Phone is empty
                  → Fatal: false (Go falls back to template messages)

"SMS_FAILED"      → Africa's Talking rejected the SMS
                  → Phone contains the specific number that failed
                  → Fatal: false (one SMS failure doesn't stop the pipeline)