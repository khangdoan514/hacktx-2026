import type { AuthConfig } from "convex/server";

const issuer = process.env.CONVEX_JWT_ISSUER ?? "";
const jwks = process.env.CONVEX_JWT_JWKS ?? "";
const audience = process.env.CONVEX_JWT_AUDIENCE ?? "authenticated";
const algorithm = process.env.CONVEX_JWT_ALGORITHM === "ES256" ? "ES256" : "RS256";

// Supabase Auth publishes RS256 or ES256 keys. Convex cannot verify the local HS256 test secret.
const providers: AuthConfig["providers"] =
  issuer && jwks
    ? [
        {
          type: "customJwt",
          issuer,
          jwks,
          applicationID: audience,
          algorithm,
        },
      ]
    : [];

export default { providers } satisfies AuthConfig;
