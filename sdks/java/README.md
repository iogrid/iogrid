# iogrid Java SDK

Official Java SDK for the [iogrid](https://iogrid.org) customer API.

```kotlin
// build.gradle.kts
dependencies {
    implementation("com.iogrid:sdk:0.1.0")
}
```

```xml
<!-- pom.xml -->
<dependency>
  <groupId>com.iogrid</groupId>
  <artifactId>sdk</artifactId>
  <version>0.1.0</version>
</dependency>
```

Java 17+. Built on OkHttp 4 + Jackson 2.

## Quick start

```java
import com.iogrid.sdk.IogridClient;
import com.iogrid.sdk.Types;

var iogrid = IogridClient.builder()
    .apiKey(System.getenv("IOGRID_API_KEY"))
    .build();

Types.Workload w = iogrid.createWorkload(new Types.CreateWorkloadRequest(
    Types.WorkloadType.BANDWIDTH, null, null,
    new Types.BandwidthRequest("https://example.com", null, null, null, null, null),
    null, null, null));
System.out.println(w.id() + " " + w.status());
```

## Examples

### 1. Submit a Docker workload

```java
Types.Workload w = iogrid.createWorkload(new Types.CreateWorkloadRequest(
    Types.WorkloadType.DOCKER, null, null, null,
    new Types.DockerRequest(
        "ghcr.io/example/scraper@sha256:abc...",
        java.util.List.of("./run.sh"),
        java.util.Map.of("CONCURRENCY", "4"),
        900L, 2, 1024L, null),
    null, null));
```

### 2. Stream a workload to completion

```java
iogrid.streamWorkloadEvents(w.id(), ev ->
    System.out.printf("[%s] %s — %s%n", ev.occurredAt(), ev.newStatus(),
        ev.note() == null ? "" : ev.note()));
```

### 3. Rotate API keys

```java
Types.CreatedApiKey created = iogrid.createApiKey(
    new Types.CreateApiKeyRequest("ci-pipeline-2026", null, null));
String secret = created.secret(); // only returned at creation

for (var k : iogrid.listApiKeys()) {
    System.out.println(k.id() + " " + k.name() + " " + k.prefix());
}

iogrid.deleteApiKey("00000000-0000-0000-0000-000000000000");
```

### 4. Pull usage and invoices

```java
var opts = new IogridClient.GetUsageOptions();
opts.windowStart = java.time.Instant.parse("2026-01-01T00:00:00Z");
opts.windowEnd   = java.time.Instant.parse("2026-02-01T00:00:00Z");
opts.type = Types.WorkloadType.BANDWIDTH;
opts.pageSize = 200;

long totalBytes = iogrid.getUsage(opts).stream().mapToLong(Types.UsageRecord::quantity).sum();
System.out.printf("bandwidth in January: %.2f GB%n", totalBytes / 1e9);

for (var inv : iogrid.getInvoices(null)) {
    System.out.println(inv.id() + " " + inv.status() + " " + inv.total().micros());
}
```

### 5. Error handling

```java
import com.iogrid.sdk.IogridException;

try {
    iogrid.createWorkload(/* … */);
} catch (IogridException ex) {
    switch (ex.code()) {
        case IogridException.CODE_INVALID_ARGUMENT -> System.out.println("field: " + ex.fieldPath());
        case IogridException.CODE_ABUSE_RATE_LIMITED -> {
            int delay = ex.retryAfterSeconds();
            if (delay > 0) System.out.println("retry in " + delay + "s");
        }
        default -> System.out.println("iogrid: " + ex.code() + " " + ex.getMessage() + " (reqId=" + ex.requestId() + ")");
    }
}
```

## License

Apache-2.0
