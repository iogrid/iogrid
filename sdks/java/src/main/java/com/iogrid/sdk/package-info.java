/**
 * Official Java SDK for the iogrid customer API.
 *
 * <p>Entry point: {@link com.iogrid.sdk.IogridClient}. Built on OkHttp 4
 * and Jackson 2; targets Java 17+.
 *
 * <pre>{@code
 * var c = IogridClient.builder()
 *     .apiKey(System.getenv("IOGRID_API_KEY"))
 *     .build();
 * Workload w = c.createWorkload(new CreateWorkloadRequest(
 *     WorkloadType.BANDWIDTH, null, null,
 *     new BandwidthRequest("https://example.com", null, null, null, null, null),
 *     null, null, null));
 * }</pre>
 *
 * <p>Methods exposed: {@code createWorkload}, {@code getWorkload},
 * {@code listWorkloads}, {@code cancelWorkload},
 * {@code streamWorkloadEvents}, {@code createApiKey}, {@code listApiKeys},
 * {@code deleteApiKey}, {@code getUsage}, {@code getInvoices}.
 */
package com.iogrid.sdk;
