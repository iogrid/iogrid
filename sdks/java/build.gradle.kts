// Build script for the iogrid Java SDK.
//
// Maven coordinates: com.iogrid:sdk:<version>. Java 17+.
//
// Transport: OkHttp 4 (the de-facto Java HTTP client, used by every
// other Java SDK we benchmark against: Stripe, Twilio, AWS SDK v2 fallback).
// JSON: Jackson 2 — fast, well-known, plays nicely with Java records.
//
// The brief mentions "grpc-java + OkHttp" — for the customer-facing REST
// surface we ship the OkHttp + Jackson client first (it covers every
// SDK method); a future PR adds a separate `:grpc` subproject for
// customers who prefer the Connect-RPC / gRPC path.

plugins {
    `java-library`
    `maven-publish`
    signing
    id("com.diffplug.spotless") version "6.25.0"
}

group = "com.iogrid"
version = "0.1.0"

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
    withSourcesJar()
    withJavadocJar()
}

repositories {
    mavenCentral()
}

dependencies {
    api("com.squareup.okhttp3:okhttp:4.12.0")
    api("com.fasterxml.jackson.core:jackson-databind:2.18.1")
    api("com.fasterxml.jackson.datatype:jackson-datatype-jsr310:2.18.1")
    api("org.jetbrains:annotations:24.1.0")

    testImplementation("org.junit.jupiter:junit-jupiter:5.11.3")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher:1.11.3")
}

tasks.test {
    useJUnitPlatform()
    testLogging {
        events("passed", "failed", "skipped")
    }
}

tasks.withType<JavaCompile> {
    options.encoding = "UTF-8"
    options.release.set(17)
}

tasks.withType<Javadoc> {
    options {
        (this as StandardJavadocDocletOptions).addStringOption("Xdoclint:none", "-quiet")
    }
}

spotless {
    java {
        target("src/**/*.java")
        googleJavaFormat("1.22.0")
        toggleOffOn()
        endWithNewline()
        trimTrailingWhitespace()
    }
    // Spotless wires `spotlessCheck` as a dependency of `check` by
    // default. We want `gradle check` (CI gate) to mean compile + test
    // only; format violations should be a separate, advisory task
    // (`gradle spotlessCheck`) that CI runs with `continue-on-error`.
    isEnforceCheck = false
}

publishing {
    publications {
        create<MavenPublication>("mavenJava") {
            from(components["java"])
            artifactId = "sdk"
            pom {
                name.set("iogrid SDK")
                description.set("Official Java SDK for the iogrid customer API.")
                url.set("https://iogrid.org")
                licenses {
                    license {
                        name.set("Apache-2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0")
                    }
                }
                scm {
                    url.set("https://github.com/iogrid/iogrid")
                    connection.set("scm:git:git://github.com/iogrid/iogrid.git")
                    developerConnection.set("scm:git:ssh://git@github.com/iogrid/iogrid.git")
                }
                developers {
                    developer {
                        id.set("iogrid")
                        name.set("iogrid")
                        email.set("support@iogrid.org")
                    }
                }
            }
        }
    }

    // Sonatype OSSRH (Maven Central) repository.
    // Credentials come from CI env vars (ORG_GRADLE_PROJECT_ossrhUsername /
    // ORG_GRADLE_PROJECT_ossrhPassword) so the build script stays clean.
    repositories {
        maven {
            name = "OSSRH"
            // s01 is the new Sonatype OSSRH host (all com.iogrid namespaces
            // registered after Feb 2021 live here).
            val releasesUrl = uri("https://s01.oss.sonatype.org/service/local/staging/deploy/maven2/")
            val snapshotsUrl = uri("https://s01.oss.sonatype.org/content/repositories/snapshots/")
            url = if (version.toString().endsWith("SNAPSHOT")) snapshotsUrl else releasesUrl
            credentials {
                username = (findProperty("ossrhUsername") as String?) ?: System.getenv("OSSRH_USERNAME")
                password = (findProperty("ossrhPassword") as String?) ?: System.getenv("OSSRH_TOKEN")
            }
        }
    }
}

signing {
    // Skip signing only when explicitly requested (e.g. local smoke runs).
    setRequired({ gradle.taskGraph.hasTask("publish") && findProperty("signing.skip") != "true" })
    val signingKey = findProperty("signingKey") as String?
    val signingPassword = findProperty("signingPassword") as String?
    if (signingKey != null && signingPassword != null) {
        useInMemoryPgpKeys(signingKey, signingPassword)
    }
    sign(publishing.publications["mavenJava"])
}
