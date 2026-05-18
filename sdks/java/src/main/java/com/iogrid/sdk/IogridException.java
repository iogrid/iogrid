package com.iogrid.sdk;

import java.util.Collections;
import java.util.Map;

/**
 * Thrown for non-2xx HTTP responses returned by the iogrid API.
 *
 * <p>Switch on {@link #code()} rather than parsing the human message;
 * codes mirror {@code iogrid.common.v1.ErrorCode}.
 *
 * <p>Common code values are exposed as {@code public static final}
 * constants on this class so callers do not need a separate enum.
 */
public class IogridException extends RuntimeException {

  /** Stable machine-readable error code constants. */
  public static final String CODE_INVALID_ARGUMENT = "INVALID_ARGUMENT";
  public static final String CODE_NOT_FOUND = "NOT_FOUND";
  public static final String CODE_ALREADY_EXISTS = "ALREADY_EXISTS";
  public static final String CODE_PERMISSION_DENIED = "PERMISSION_DENIED";
  public static final String CODE_UNAUTHENTICATED = "UNAUTHENTICATED";
  public static final String CODE_RESOURCE_EXHAUSTED = "RESOURCE_EXHAUSTED";
  public static final String CODE_FAILED_PRECONDITION = "FAILED_PRECONDITION";
  public static final String CODE_INTERNAL = "INTERNAL";
  public static final String CODE_UNAVAILABLE = "UNAVAILABLE";
  public static final String CODE_DEADLINE_EXCEEDED = "DEADLINE_EXCEEDED";
  public static final String CODE_ABUSE_BLOCKED = "ABUSE_BLOCKED";
  public static final String CODE_ABUSE_RATE_LIMITED = "ABUSE_RATE_LIMITED";
  public static final String CODE_STEP_UP_REQUIRED = "STEP_UP_REQUIRED";

  private static final long serialVersionUID = 1L;

  private final int status;
  private final String code;
  private final String fieldPath;
  private final Map<String, String> metadata;
  private final String requestId;

  public IogridException(int status, Types.ErrorEnvelope envelope) {
    super(envelope != null && envelope.message() != null ? envelope.message() : ("iogrid: HTTP " + status));
    this.status = status;
    this.code = envelope != null && envelope.code() != null ? envelope.code() : CODE_INTERNAL;
    this.fieldPath = envelope == null ? null : envelope.fieldPath();
    this.metadata = envelope == null || envelope.metadata() == null
        ? Collections.emptyMap()
        : Collections.unmodifiableMap(envelope.metadata());
    this.requestId = envelope == null ? null : envelope.requestId();
  }

  public int status() {
    return status;
  }

  public String code() {
    return code;
  }

  public String fieldPath() {
    return fieldPath;
  }

  public Map<String, String> metadata() {
    return metadata;
  }

  public String requestId() {
    return requestId;
  }

  /**
   * Server-suggested retry delay (seconds) for rate-limit refusals;
   * returns {@code -1} if absent or unparseable.
   */
  public int retryAfterSeconds() {
    String raw = metadata.getOrDefault("retry_after_seconds", metadata.get("retryAfterSeconds"));
    if (raw == null || raw.isEmpty()) return -1;
    try {
      return Integer.parseInt(raw);
    } catch (NumberFormatException ignored) {
      return -1;
    }
  }
}
