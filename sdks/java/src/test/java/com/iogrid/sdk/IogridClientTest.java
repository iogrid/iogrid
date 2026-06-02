package com.iogrid.sdk;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class IogridClientTest {

  private MockWebServer server;
  private IogridClient client;
  private ObjectMapper mapper;

  @BeforeEach
  void setUp() throws IOException {
    server = new MockWebServer();
    server.start();
    client = IogridClient.builder()
        .apiKey("iog_test")
        .baseUrl(server.url("/").toString())
        .build();
    mapper = new ObjectMapper()
        .registerModule(new JavaTimeModule())
        .disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS);
  }

  @AfterEach
  void tearDown() throws IOException {
    client.close();
    server.shutdown();
  }

  @Test
  void createWorkloadPostsJson() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(201)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"id\":\"w1\",\"workspaceId\":\"ws\",\"type\":\"BANDWIDTH\",\"status\":\"queued\"}"));

    Types.Workload w = client.createWorkload(new Types.CreateWorkloadRequest(
        Types.WorkloadType.BANDWIDTH, null, null,
        new Types.BandwidthRequest("https://example.com", null, null, null, null, null),
        null, null, null));

    assertEquals("w1", w.id());
    RecordedRequest req = server.takeRequest();
    assertEquals("POST", req.getMethod());
    assertEquals("/v1/workloads", req.getPath());
    assertEquals("Bearer iog_test", req.getHeader("Authorization"));
    assertTrue(req.getBody().readUtf8().contains("\"targetUrl\":\"https://example.com\""));
  }

  @Test
  void getWorkloadReturnsResponse() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"workload\":{\"id\":\"abc\",\"workspaceId\":\"ws\",\"type\":\"DOCKER\",\"status\":\"queued\"}}"));

    Types.GetWorkloadResponse r = client.getWorkload("abc");
    assertEquals("abc", r.workload().id());
  }

  @Test
  void listWorkloadsDropsEmptyParams() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"workloads\":[]}"));

    IogridClient.ListWorkloadsOptions opts = new IogridClient.ListWorkloadsOptions();
    opts.pageSize = 50;
    opts.type = Types.WorkloadType.DOCKER;
    client.listWorkloads(opts);

    RecordedRequest req = server.takeRequest();
    String path = req.getPath();
    assertTrue(path.contains("pageSize=50"), path);
    assertTrue(path.contains("type=DOCKER"), path);
    assertFalse(path.contains("status="), path);
  }

  @Test
  void cancelWorkloadWithReason() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"id\":\"w1\",\"workspaceId\":\"ws\",\"type\":\"BANDWIDTH\",\"status\":\"cancelled\"}"));
    Types.Workload w = client.cancelWorkload("w1", "user requested");
    assertEquals("cancelled", w.status());
    RecordedRequest req = server.takeRequest();
    assertEquals("DELETE", req.getMethod());
    assertTrue(req.getPath().contains("reason=user%20requested"), req.getPath());
  }

  @Test
  void deleteApiKey204() throws Exception {
    server.enqueue(new MockResponse().setResponseCode(204));
    assertDoesNotThrow(() -> client.deleteApiKey("k1"));
  }

  @Test
  void listApiKeysUnwrapsEnvelope() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"keys\":[{\"id\":\"k1\",\"name\":\"ci\",\"prefix\":\"iog_abcd\",\"createdAt\":\"2026-01-01T00:00:00Z\"}]}"));
    List<Types.ApiKeyMetadata> keys = client.listApiKeys();
    assertEquals(1, keys.size());
    assertEquals("iog_abcd", keys.get(0).prefix());
  }

  @Test
  void errorOnNon2xx() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(400)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"code\":\"INVALID_ARGUMENT\",\"message\":\"bad target\",\"fieldPath\":\"bandwidth.targetUrl\",\"requestId\":\"req-123\"}"));
    IogridException ex = assertThrows(IogridException.class, () ->
        client.createWorkload(new Types.CreateWorkloadRequest(
            Types.WorkloadType.BANDWIDTH, null, null,
            new Types.BandwidthRequest("", null, null, null, null, null),
            null, null, null)));
    assertEquals(400, ex.status());
    assertEquals("INVALID_ARGUMENT", ex.code());
    assertEquals("bandwidth.targetUrl", ex.fieldPath());
    assertEquals("req-123", ex.requestId());
  }

  @Test
  void streamWorkloadEventsParsesSse() throws Exception {
    String body =
        "data: {\"workloadId\":\"w1\",\"newStatus\":\"queued\",\"occurredAt\":\"2026-01-01T00:00:00Z\"}\n\n"
            + "data: {\"workloadId\":\"w1\",\"newStatus\":\"running\",\"occurredAt\":\"2026-01-01T00:00:01Z\"}\n\n"
            + "data: {\"workloadId\":\"w1\",\"newStatus\":\"succeeded\",\"occurredAt\":\"2026-01-01T00:00:02Z\"}\n\n";
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "text/event-stream")
        .setBody(body));

    List<String> seen = new ArrayList<>();
    client.streamWorkloadEvents("w1", ev -> seen.add(ev.newStatus()));
    assertEquals(List.of("queued", "running", "succeeded"), seen);
  }

  @Test
  void streamWorkloadEvents4xxThrows() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(404)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"code\":\"NOT_FOUND\",\"message\":\"no such workload\"}"));
    IogridException ex = assertThrows(IogridException.class, () ->
        client.streamWorkloadEvents("nope", e -> {}));
    assertEquals("NOT_FOUND", ex.code());
  }

  @Test
  void builderRequiresApiKey() {
    assertThrows(IllegalArgumentException.class, () -> IogridClient.builder().build());
  }

  @Test
  void userAgentHeaderPresent() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(200)
        .setHeader("Content-Type", "application/json")
        .setBody("{\"workloads\":[]}"));
    client.listWorkloads(null);
    RecordedRequest req = server.takeRequest();
    String ua = req.getHeader("User-Agent");
    assertNotNull(ua);
    assertTrue(ua.startsWith("iogrid-sdk-java/"), ua);
  }

  @Test
  void retryAfterSecondsExtraction() {
    Types.ErrorEnvelope env = new Types.ErrorEnvelope(
        "ABUSE_RATE_LIMITED", "x", null,
        java.util.Map.of("retry_after_seconds", "12"), null);
    IogridException ex = new IogridException(429, env);
    assertEquals(12, ex.retryAfterSeconds());
  }

  @Test
  void requestMobileSessionPostsSnakeCaseAndParsesResponse() throws Exception {
    server.enqueue(new MockResponse()
        .setResponseCode(201)
        .setHeader("Content-Type", "application/json")
        .setBody("{"
            + "\"session_id\":\"sess-1\","
            + "\"peer_public_key\":\"peer-pubkey-b64\","
            + "\"peer_endpoint\":\"203.0.113.7:51820\","
            + "\"customer_inner_cidr\":\"10.244.7.4/32\","
            + "\"allowed_ips\":\"0.0.0.0/0\","
            + "\"dns_servers\":[\"1.1.1.1\",\"1.0.0.1\"],"
            + "\"region\":\"eu-central\","
            + "\"expires_at\":\"2026-06-04T00:00:00Z\","
            + "\"quota_state\":\"QUOTA_STATE_HEALTHY\""
            + "}"));

    Types.RequestMobileSessionResponse r = client.requestMobileSession(
        new Types.RequestMobileSessionRequest(
            "11111111-2222-3333-4444-555555555555",
            "auto",
            "wg-pubkey-b64",
            null,
            null));

    assertEquals("sess-1", r.sessionId());
    assertEquals("203.0.113.7:51820", r.peerEndpoint());
    assertEquals("10.244.7.4/32", r.customerInnerCidr());
    assertEquals(List.of("1.1.1.1", "1.0.0.1"), r.dnsServers());
    assertEquals(Types.QuotaState.QUOTA_STATE_HEALTHY, r.quotaState());
    assertEquals(Instant.parse("2026-06-04T00:00:00Z"), r.expiresAt());

    RecordedRequest req = server.takeRequest();
    assertEquals("POST", req.getMethod());
    assertEquals("/v1/vpn/sessions/mobile", req.getPath());
    String sent = req.getBody().readUtf8();
    // Wire MUST use snake_case (vpn-svc handler verbatim).
    assertTrue(sent.contains("\"customer_id\""), sent);
    assertTrue(sent.contains("\"client_public_key\""), sent);
    assertTrue(sent.contains("\"region\":\"auto\""), sent);
    // Optional fields with null values must NOT appear on the wire.
    assertFalse(sent.contains("\"api_key\""), sent);
    assertFalse(sent.contains("\"payment_authorization\""), sent);
  }

  @Test
  void requestMobileSession503SurfacesIogridException() {
    server.enqueue(new MockResponse()
        .setResponseCode(503)
        .setHeader("Content-Type", "application/json")
        .setHeader("Retry-After", "15")
        .setBody("{\"error\":\"no_peer_available\",\"detail\":\"no healthy peer\",\"region\":\"us-west\",\"retry_after_sec\":15}"));

    IogridException ex = assertThrows(IogridException.class, () ->
        client.requestMobileSession(new Types.RequestMobileSessionRequest(
            "11111111-2222-3333-4444-555555555555",
            null,
            "wg-pubkey-b64",
            null,
            null)));
    assertEquals(503, ex.status());
  }

  @Test
  void requestMobileSessionRequiresCustomerIdAndKey() {
    assertThrows(IllegalArgumentException.class, () ->
        client.requestMobileSession(new Types.RequestMobileSessionRequest(
            "", null, "wg", null, null)));
    assertThrows(IllegalArgumentException.class, () ->
        client.requestMobileSession(new Types.RequestMobileSessionRequest(
            "cust", null, "", null, null)));
  }

  @Test
  void roundTripInstantSerialization() throws Exception {
    // Sanity check: Jackson + JavaTimeModule keep ISO-8601 timestamps.
    Types.WorkloadEvent ev = new Types.WorkloadEvent(
        "w1", "queued", Instant.parse("2026-01-01T00:00:00Z"), null);
    String s = mapper.writeValueAsString(ev);
    assertTrue(s.contains("2026-01-01T00:00:00Z"), s);
  }
}
