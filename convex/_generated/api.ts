import { anyApi } from "convex/server";
import type { FunctionReference } from "convex/server";

type AnyApi = {
  hints: {
    listForWorkspace: FunctionReference<"query", "public">;
    push: FunctionReference<"mutation", "internal">;
  };
};

export const api = anyApi as unknown as Pick<AnyApi, "hints"> & {
  hints: { listForWorkspace: AnyApi["hints"]["listForWorkspace"] };
};
export const internal = anyApi as unknown as {
  hints: { push: AnyApi["hints"]["push"] };
};
