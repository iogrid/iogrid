// Compile + run with the iogrid Java SDK on the classpath:
//
//   javac -cp 'sdks/java/build/libs/*' examples/java-api-key-rotation.java
//   java  -cp '.:sdks/java/build/libs/*' ApiKeyRotation
//
// Requires IOGRID_API_KEY in the environment.

import com.iogrid.sdk.IogridClient;
import com.iogrid.sdk.IogridException;
import com.iogrid.sdk.Types;

import java.time.Duration;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.List;

public class ApiKeyRotation {

  public static void main(String[] args) throws Exception {
    String apiKey = System.getenv("IOGRID_API_KEY");
    if (apiKey == null || apiKey.isEmpty()) {
      System.err.println("set IOGRID_API_KEY first");
      System.exit(1);
    }

    try (var iogrid = IogridClient.builder()
        .apiKey(apiKey)
        .timeout(Duration.ofSeconds(30))
        .userAgent("api-key-rotation-example/1.0")
        .build()) {

      // 1. Mint a fresh key with a 90-day TTL.
      Types.CreatedApiKey fresh = iogrid.createApiKey(new Types.CreateApiKeyRequest(
          "ci-pipeline-rotation",
          Instant.now().plus(90, ChronoUnit.DAYS),
          List.of("workloads:submit", "billing:read")));
      System.out.println("minted " + fresh.prefix() + " expires=" + fresh.expiresAt());
      System.out.println("SECRET (save it now): " + fresh.secret());

      // 2. List existing keys; revoke anything older than 180 days.
      Instant cutoff = Instant.now().minus(180, ChronoUnit.DAYS);
      for (Types.ApiKeyMetadata k : iogrid.listApiKeys()) {
        if (k.createdAt() != null && k.createdAt().isBefore(cutoff) && !k.id().equals(fresh.id())) {
          try {
            iogrid.deleteApiKey(k.id());
            System.out.println("revoked " + k.prefix() + " (createdAt=" + k.createdAt() + ")");
          } catch (IogridException ex) {
            System.err.println("could not revoke " + k.id() + ": " + ex.code());
          }
        }
      }
    }
  }
}
