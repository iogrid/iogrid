package com.iogrid.sdk;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.time.Instant;
import java.util.List;
import java.util.Map;

/**
 * Wire types for the iogrid customer API.
 *
 * <p>We use Java 17 records — JSON property names match the OpenAPI
 * spec at {@code proto/gen/openapi/iogrid.yaml} (camelCase). Jackson is
 * configured in {@link IogridClient} to drop {@code null} fields on
 * serialisation, so {@code null}-valued record components match the
 * proto3 "field unset" semantics.
 */
public final class Types {

  private Types() {}

  /** Kinds of work the grid can route to providers. */
  public enum WorkloadType {
    BANDWIDTH,
    DOCKER,
    GPU,
    IOS_BUILD;
  }

  /** Scheduler urgency hint among queued jobs. */
  public enum WorkloadPriority {
    LOW,
    NORMAL,
    HIGH;
  }

  /** Invoice status mirroring Stripe's subset we expose. */
  public enum InvoiceStatus {
    @JsonProperty("draft") DRAFT,
    @JsonProperty("open") OPEN,
    @JsonProperty("paid") PAID,
    @JsonProperty("void") VOID,
    @JsonProperty("uncollectible") UNCOLLECTIBLE;
  }

  /**
   * Fixed-precision monetary amount. Micros = millionths of the major
   * currency unit; 12.34 USD == {@code new Money("USD", 12_340_000L)}.
   */
  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record Money(String currency, long micros) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record BandwidthRequest(
      String targetUrl,
      String method,
      String sessionId,
      String preferredRegion,
      String category,
      Money maxSpend) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record DockerRequest(
      String image,
      List<String> command,
      Map<String, String> env,
      Long timeoutSeconds,
      Integer minCpuCores,
      Long minMemoryMib,
      Long minGpuMemoryMib) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record GpuRequest(
      String image,
      List<String> command,
      Map<String, String> env,
      Long timeoutSeconds,
      Long minVramMib,
      List<String> allowedVendors) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record IosBuildRequest(
      String sourceTarballS3Key,
      String tartImage,
      List<String> buildCommands,
      String artifactS3Bucket,
      String artifactS3Prefix) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record CreateWorkloadRequest(
      WorkloadType type,
      WorkloadPriority priority,
      Map<String, String> labels,
      BandwidthRequest bandwidth,
      DockerRequest docker,
      GpuRequest gpu,
      IosBuildRequest iosBuild) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record Workload(
      String id,
      String workspaceId,
      String submittedByUserId,
      WorkloadType type,
      WorkloadPriority priority,
      String status,
      Instant submittedAt,
      Instant startedAt,
      Instant finishedAt,
      Map<String, String> labels,
      BandwidthRequest bandwidth,
      DockerRequest docker,
      GpuRequest gpu,
      IosBuildRequest iosBuild) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record WorkloadResult(
      String workloadId,
      String terminalStatus,
      Integer exitCode,
      String logsS3Key,
      Long bytesIn,
      Long bytesOut,
      List<String> artifactS3Keys,
      Money cost,
      Instant completedAt) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record GetWorkloadResponse(Workload workload, WorkloadResult result) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record WorkloadEvent(
      String workloadId, String newStatus, Instant occurredAt, String note) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ListWorkloadsResponse(List<Workload> workloads, String nextPageToken) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record CreateApiKeyRequest(String name, Instant expiresAt, List<String> scopes) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ApiKeyMetadata(
      String id,
      String name,
      String prefix,
      Instant createdAt,
      Instant lastUsedAt,
      Instant expiresAt,
      List<String> scopes) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record CreatedApiKey(
      String id,
      String name,
      String prefix,
      Instant createdAt,
      Instant lastUsedAt,
      Instant expiresAt,
      List<String> scopes,
      /** Only returned at creation time. Store securely. */
      String secret) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ListApiKeysResponse(List<ApiKeyMetadata> keys, String nextPageToken) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record UsageRecord(
      String id,
      String workloadId,
      WorkloadType type,
      long quantity,
      Money cost,
      Instant recordedAt) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ListUsageResponse(
      List<UsageRecord> usage, String nextPageToken, Money pageSubtotal) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record Invoice(
      String id,
      Instant periodStart,
      Instant periodEnd,
      Money subtotal,
      Money tax,
      Money total,
      InvoiceStatus status,
      Instant issuedAt,
      Instant paidAt,
      String hostedInvoiceUrl) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ListInvoicesResponse(List<Invoice> invoices, String nextPageToken) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record ErrorEnvelope(
      String code,
      String message,
      String fieldPath,
      Map<String, String> metadata,
      String requestId) {}
}
