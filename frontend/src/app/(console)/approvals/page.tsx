"use client";

import ApprovalInbox from "@/components/approvals/ApprovalInbox";
import PageHeader from "@/components/shell/PageHeader";

export default function ApprovalsPage() {
  return (
    <>
      <PageHeader
        title="Approvals"
        sub="Protected writes stay waiting until a different person approves or denies them."
      />
      <ApprovalInbox />
    </>
  );
}
