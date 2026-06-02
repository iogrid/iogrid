package com.iogrid.sdk;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;

import okhttp3.HttpUrl;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.function.Consumer;

/**
 * Top-level iogrid customer SDK client. Thread-safe; create one
 * {@link IogridClient} per process and share.
 *
 * <p>Construct via {@link #builder()}; closeable via {@link #close()}
 * (the underlying OkHttp dispatcher is shut down).
 */
public final class IogridClient implements AutoCloseable {

  /** SDK version, surfaced in the User-Agent header. */
  public static final String VERSION = "0.1.0";

  private static final MediaType JSON = MediaType.get("application/json; charset=utf-8");
  private static final String DEFAULT_BASE_URL = "https://api.iogrid.org";

  private final OkHttpClient http;
  private final HttpUrl baseUrl;
  private final String apiKey;
  private final String userAgent;
  private final ObjectMapper mapper;
  private final boolean ownsHttp;

  private IogridClient(Builder b) {
    this.apiKey = Objects.requireNonNull(b.apiKey, "apiKey");
    this.baseUrl = HttpUrl.get(stripTrailingSlash(b.baseUrl == null ? DEFAULT_BASE_URL : b.baseUrl));
    if (b.http != null) {
      this.http = b.http;
      this.ownsHttp = false;
    } else {
      this.http = new OkHttpClient.Builder()
          .callTimeout(b.timeout == null ? Duration.ofSeconds(30) : b.timeout)
          .build();
      this.ownsHttp = true;
    }
    String ua = "iogrid-sdk-java/" + VERSION;
    if (b.userAgent != null && !b.userAgent.isEmpty()) {
      ua = ua + " (" + b.userAgent + ")";
    }
    this.userAgent = ua;
    this.mapper = new ObjectMapper()
        .registerModule(new JavaTimeModule())
        .disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS)
        .disable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
  }

  private static String stripTrailingSlash(String s) {
    int n = s.length();
    while (n > 0 && s.charAt(n - 1) == '/') n--;
    return s.substring(0, n);
  }

  public static Builder builder() {
    return new Builder();
  }

  // --- Workloads ---------------------------------------------------------

  /** Submit a new workload. */
  public Types.Workload createWorkload(Types.CreateWorkloadRequest body) throws IOException {
    return doJson("POST", "/v1/workloads", body, null, Types.Workload.class);
  }

  /** Retrieve a workload by id. */
  public Types.GetWorkloadResponse getWorkload(String id) throws IOException {
    return doJson("GET", "/v1/workloads/" + encode(id), null, null, Types.GetWorkloadResponse.class);
  }

  /** List workloads in the caller's workspace. */
  public Types.ListWorkloadsResponse listWorkloads(ListWorkloadsOptions opts) throws IOException {
    HttpUrl.Builder q = baseUrl.newBuilder().addPathSegments("v1/workloads");
    if (opts != null) {
      if (opts.pageSize != null) q.addQueryParameter("pageSize", String.valueOf(opts.pageSize));
      if (opts.pageToken != null) q.addQueryParameter("pageToken", opts.pageToken);
      if (opts.type != null) q.addQueryParameter("type", opts.type.name());
      if (opts.status != null) q.addQueryParameter("status", opts.status);
      if (opts.submittedAfter != null) q.addQueryParameter("submittedAfter", opts.submittedAfter.toString());
      if (opts.submittedBefore != null) q.addQueryParameter("submittedBefore", opts.submittedBefore.toString());
    }
    return doJsonUrl("GET", q.build(), null, Types.ListWorkloadsResponse.class);
  }

  /** Cancel a queued or running workload. */
  public Types.Workload cancelWorkload(String id, String reason) throws IOException {
    HttpUrl.Builder q = baseUrl.newBuilder().addPathSegments("v1/workloads/" + encode(id));
    if (reason != null && !reason.isEmpty()) q.addQueryParameter("reason", reason);
    return doJsonUrl("DELETE", q.build(), null, Types.Workload.class);
  }

  /**
   * Open an SSE stream of workload state transitions. Each event is
   * passed to {@code onEvent}; the call returns when the server closes
   * the stream (typically on terminal status).
   */
  public void streamWorkloadEvents(String id, Consumer<Types.WorkloadEvent> onEvent) throws IOException {
    HttpUrl url = baseUrl.newBuilder().addPathSegments("v1/workloads/" + encode(id) + "/events").build();
    Request req = baseRequest(url, "GET", null).header("Accept", "text/event-stream").build();
    try (Response resp = http.newCall(req).execute()) {
      if (!resp.isSuccessful()) throw decodeError(resp);
      if (resp.body() == null) throw new IogridException(resp.code(),
          new Types.ErrorEnvelope("INTERNAL", "stream: empty body", null, null, null));
      try (BufferedReader br = new BufferedReader(
          new InputStreamReader(resp.body().byteStream(), StandardCharsets.UTF_8))) {
        List<String> dataLines = new ArrayList<>();
        String line;
        while ((line = br.readLine()) != null) {
          if (line.isEmpty()) {
            if (!dataLines.isEmpty()) {
              String payload = String.join("\n", dataLines);
              dataLines.clear();
              try {
                Types.WorkloadEvent ev = mapper.readValue(payload, Types.WorkloadEvent.class);
                onEvent.accept(ev);
              } catch (IOException ignored) {
                // Skip malformed events; the server is supposed to emit valid JSON.
              }
            }
            continue;
          }
          if (line.startsWith(":")) continue; // SSE comment
          if (line.startsWith("data:")) {
            String d = line.substring(5);
            while (d.startsWith(" ")) d = d.substring(1);
            dataLines.add(d);
          }
        }
      }
    }
  }

  // --- API keys ----------------------------------------------------------

  /** Mint a new API key. The {@code secret} is returned only at creation. */
  public Types.CreatedApiKey createApiKey(Types.CreateApiKeyRequest body) throws IOException {
    return doJson("POST", "/v1/keys", body, null, Types.CreatedApiKey.class);
  }

  /** List API keys for the caller's workspace (metadata only). */
  public List<Types.ApiKeyMetadata> listApiKeys() throws IOException {
    Types.ListApiKeysResponse r = doJson("GET", "/v1/keys", null, null, Types.ListApiKeysResponse.class);
    return r == null || r.keys() == null ? List.of() : r.keys();
  }

  /** Revoke an API key. */
  public void deleteApiKey(String id) throws IOException {
    doJson("DELETE", "/v1/keys/" + encode(id), null, null, Void.class);
  }

  // --- Billing -----------------------------------------------------------

  /** Paged list of metered usage records. */
  public List<Types.UsageRecord> getUsage(GetUsageOptions opts) throws IOException {
    HttpUrl.Builder q = baseUrl.newBuilder().addPathSegments("v1/usage");
    if (opts != null) {
      if (opts.pageSize != null) q.addQueryParameter("pageSize", String.valueOf(opts.pageSize));
      if (opts.pageToken != null) q.addQueryParameter("pageToken", opts.pageToken);
      if (opts.type != null) q.addQueryParameter("type", opts.type.name());
      if (opts.windowStart != null) q.addQueryParameter("windowStart", opts.windowStart.toString());
      if (opts.windowEnd != null) q.addQueryParameter("windowEnd", opts.windowEnd.toString());
    }
    Types.ListUsageResponse r = doJsonUrl("GET", q.build(), null, Types.ListUsageResponse.class);
    return r == null || r.usage() == null ? List.of() : r.usage();
  }

  /** Paged list of invoices. */
  public List<Types.Invoice> getInvoices(GetInvoicesOptions opts) throws IOException {
    HttpUrl.Builder q = baseUrl.newBuilder().addPathSegments("v1/invoices");
    if (opts != null) {
      if (opts.pageSize != null) q.addQueryParameter("pageSize", String.valueOf(opts.pageSize));
      if (opts.pageToken != null) q.addQueryParameter("pageToken", opts.pageToken);
    }
    Types.ListInvoicesResponse r = doJsonUrl("GET", q.build(), null, Types.ListInvoicesResponse.class);
    return r == null || r.invoices() == null ? List.of() : r.invoices();
  }

  // --- Mobile VPN session bring-up ---------------------------------------

  /**
   * Request a one-shot mobile-app VPN session via {@code POST
   * /v1/vpn/sessions/mobile}. Returns the full WireGuard peer config
   * so the iOS/Android PacketTunnelProvider can call
   * {@code WireGuardAdapter.start} without a second round-trip.
   *
   * <p>Distinct from the legacy daemon-driven flow at {@code POST
   * /v1/vpn/sessions}. On 503 the SDK throws an {@link IogridException}
   * with {@code status == 503}; the server's {@code Retry-After} hint
   * defaults to 15s.
   *
   * @throws IllegalArgumentException if {@code customerId} or
   *     {@code clientPublicKey} is null/empty.
   */
  public Types.RequestMobileSessionResponse requestMobileSession(
      Types.RequestMobileSessionRequest body) throws IOException {
    if (body == null || body.customerId() == null || body.customerId().isEmpty()) {
      throw new IllegalArgumentException(
          "requestMobileSession: customerId is required");
    }
    if (body.clientPublicKey() == null || body.clientPublicKey().isEmpty()) {
      throw new IllegalArgumentException(
          "requestMobileSession: clientPublicKey is required");
    }
    return doJson("POST", "/v1/vpn/sessions/mobile", body, null,
        Types.RequestMobileSessionResponse.class);
  }

  // --- transport plumbing ------------------------------------------------

  private <T> T doJson(String method, String path, Object body, Object unusedQuery, Class<T> outType) throws IOException {
    HttpUrl url = baseUrl.newBuilder().addPathSegments(path.startsWith("/") ? path.substring(1) : path).build();
    return doJsonUrl(method, url, body, outType);
  }

  private <T> T doJsonUrl(String method, HttpUrl url, Object body, Class<T> outType) throws IOException {
    RequestBody rb = null;
    if (body != null) {
      String json = mapper.writeValueAsString(body);
      rb = RequestBody.create(json, JSON);
    } else if ("POST".equals(method) || "DELETE".equals(method)) {
      // OkHttp wants non-null body for POST; empty body is fine.
      if ("POST".equals(method)) rb = RequestBody.create(new byte[0], JSON);
    }

    Request req = baseRequest(url, method, rb).build();
    try (Response resp = http.newCall(req).execute()) {
      if (resp.code() == 204) return null;
      String text = resp.body() == null ? "" : resp.body().string();
      if (!resp.isSuccessful()) throw decodeErrorBody(resp.code(), text);
      if (outType == Void.class || text.isEmpty()) return null;
      return mapper.readValue(text, outType);
    }
  }

  private Request.Builder baseRequest(HttpUrl url, String method, RequestBody body) {
    Request.Builder b = new Request.Builder()
        .url(url)
        .header("Authorization", "Bearer " + apiKey)
        .header("Accept", "application/json")
        .header("User-Agent", userAgent);
    if ("GET".equals(method)) b.get();
    else if ("DELETE".equals(method)) b.delete(body);
    else if ("POST".equals(method)) b.post(body == null ? RequestBody.create(new byte[0], JSON) : body);
    else b.method(method, body);
    return b;
  }

  private IogridException decodeError(Response resp) throws IOException {
    String text = resp.body() == null ? "" : resp.body().string();
    return decodeErrorBody(resp.code(), text);
  }

  private IogridException decodeErrorBody(int status, String text) {
    Types.ErrorEnvelope env;
    try {
      env = text.isEmpty() ? null : mapper.readValue(text, Types.ErrorEnvelope.class);
    } catch (IOException e) {
      env = null;
    }
    if (env == null || env.code() == null) {
      env = new Types.ErrorEnvelope("INTERNAL", "HTTP " + status, null, null, null);
    }
    return new IogridException(status, env);
  }

  private static String encode(String s) {
    // OkHttp's HttpUrl.Builder.addPathSegments handles encoding when we
    // pass a single segment; we keep this helper for inline-path uses.
    return HttpUrl.parse("http://x/" + s).pathSegments().get(0);
  }

  @Override
  public void close() {
    if (!ownsHttp) return;
    http.dispatcher().executorService().shutdown();
    http.connectionPool().evictAll();
    Optional.ofNullable(http.cache()).ifPresent(cache -> {
      try { cache.close(); } catch (IOException ignored) {}
    });
  }

  // --- Options + Builder -------------------------------------------------

  public static final class ListWorkloadsOptions {
    public Integer pageSize;
    public String pageToken;
    public Types.WorkloadType type;
    public String status;
    public Instant submittedAfter;
    public Instant submittedBefore;
  }

  public static final class GetUsageOptions {
    public Integer pageSize;
    public String pageToken;
    public Types.WorkloadType type;
    public Instant windowStart;
    public Instant windowEnd;
  }

  public static final class GetInvoicesOptions {
    public Integer pageSize;
    public String pageToken;
  }

  public static final class Builder {
    private String apiKey;
    private String baseUrl;
    private String userAgent;
    private Duration timeout;
    private OkHttpClient http;

    private Builder() {}

    public Builder apiKey(String v) { this.apiKey = v; return this; }
    public Builder baseUrl(String v) { this.baseUrl = v; return this; }
    public Builder userAgent(String v) { this.userAgent = v; return this; }
    public Builder timeout(Duration v) { this.timeout = v; return this; }
    public Builder httpClient(OkHttpClient v) { this.http = v; return this; }

    public IogridClient build() {
      if (apiKey == null || apiKey.isEmpty()) {
        throw new IllegalArgumentException("IogridClient: apiKey is required");
      }
      return new IogridClient(this);
    }
  }

  /**
   * Iterable adapter for callers that prefer "for-each" over the
   * callback-style {@link #streamWorkloadEvents(String, Consumer)}.
   * Internally drains the SSE stream into a list before returning, so
   * use the callback variant for long-lived streams.
   */
  public Iterable<Types.WorkloadEvent> collectWorkloadEvents(String id) throws IOException {
    List<Types.WorkloadEvent> out = new ArrayList<>();
    streamWorkloadEvents(id, out::add);
    return () -> {
      Iterator<Types.WorkloadEvent> it = out.iterator();
      return it;
    };
  }
}
