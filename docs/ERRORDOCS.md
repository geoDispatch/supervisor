# Error Handling & Error Codes

This document defines the standard error handling philosophy, error codes, and escalation rules for the GeoDispatch Supervisor.

## Overview

The system is designed for graceful degradation. Most external failures — including CAMARA, AI Agent, and SMS Gateway errors — are non-fatal and should not halt the entire pipeline. Only critical internal failures, such as database failures, should stop execution.

## Error Code Reference

All errors broadcast to the WebSocket dashboard or logged internally must use the typed `ErrorCode` constants from the `models` package. Never pass raw strings to `ErrorUpdate.Code`.

### `models.ErrCAMARATimeout`

- **Cause:** The Nokia Network as Code (NaC) API did not respond within the configured timeout threshold, or the network dropped the connection.
- **Scope:** Device-specific or area-level
- **Phone Field:** Empty (`""`) for area-level failures (for example, QoS or geofencing), or the specific phone number when a per-device lookup fails.
- **Fatal:** `false`
- **Pipeline Behavior:** The affected device is skipped and the pipeline continues processing other devices. If area-level QoS fails, the system falls back to standard network priority.

### `models.ErrAGENT_ERROR`

- **Cause:** The Python AI Agent crashed, returned a non-200 HTTP status, or returned malformed/invalid JSON that failed to unmarshal.
- **Scope:** Batch-level
- **Phone Field:** Empty (`""`)
- **Fatal:** `false`
- **Pipeline Behavior:** The current batch of devices is skipped. The Go supervisor logs the error, broadcasts a warning to the dashboard, and continues to the next batch.  
  _Future enhancement: the Go service may fall back to hardcoded template messages if the AI service is unavailable._

### `models.ErrSMSFailed`

- **Cause:** The SMS Gateway (for example, Africa's Talking) rejected the message due to invalid formatting, insufficient sandbox credits, or carrier rejection.
- **Scope:** Device-specific
- **Phone Field:** The specific phone number in E.164 format
- **Fatal:** `false`
- **Pipeline Behavior:** The SMS action is marked as failed for that device. If the AI decision was `ActionBoth`, the `rescue_flag` operation still executes. The pipeline continues.

### `models.ErrDBError`

- **Cause:** PostgreSQL connection loss, PostGIS query failure, or a critical transaction failure, such as saving a rescue flag.
- **Scope:** System-level
- **Phone Field:** Empty (`""`) or the affected phone number if the failure occurred during a row-specific operation.
- **Fatal:** `true`
- **Pipeline Behavior:** The pipeline halts immediately. Continuing would produce corrupt, incomplete, or unlogged state. A critical alert is broadcast to the dashboard.

### `models.ErrQoSFailed`

- **Cause:** The request to upgrade network Quality of Service (QoS) for the disaster epicenter was rejected or timed out.
- **Scope:** Area-level
- **Phone Field:** Empty (`""`)
- **Fatal:** `false`
- **Pipeline Behavior:** Logged as a warning. The pipeline continues under standard, non-prioritized network conditions.

## Usage Rules & Best Practices

### Always Use Constants

Use the typed `ErrorCode` constants from the `models` package consistently:

```go
errorUpdate := models.ErrorUpdate{
	Code:  models.ErrCAMARATimeout,
	Fatal: false,
}