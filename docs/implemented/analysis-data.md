# Implemented analysis data behavior

This path is historical. It is not the primary NMC2 capability-request journey. Dashboard create, revise, and publish handlers are not part of this note.

## CSV import

`import_csv` requires these columns:

`partner_id`, `contract_id`, `contract_status`, `contract_end`, `period`, `resource`, `available_capacity`, `committed_capacity`, `consumed_capacity`, `gpu_hours`, `revenue`, `included_cost`, `currency`, `extra_allocation`.

Each row is stored. A row is quarantined when identity is missing, the partner/contract/period/resource key is duplicated in the file, `included_cost` is empty, or a numeric field is malformed. An empty cost is never stored as zero and never treated as zero later. A file whose every row is quarantined is `rejected`. Otherwise the version is `validated`. The summary counts accepted and quarantined rows.

## Registered calculation

`capacity_margin` in `backend/src/app/domain/calc.py` is the registered capacity and gross-margin check.

Gross margin is `(revenue - included_cost) / revenue`. Missing cost returns `missing_cost`. Missing revenue returns `missing_revenue`. Zero revenue returns `zero_revenue`. None of those cases substitute zero for the missing money.

Pool figures are repeated on each partner row. Available capacity and committed capacity for a resource use the maximum value seen, so repeated pool rows are not summed. Rows outside the requested period, expired contracts, duplicates, and quarantined rows are excluded and listed with a reason. Extra allocation contributes to requested capacity.

## Metrics and questions

A metric definition has immutable versions. A version can be accepted. Data questions have an answer command that can also accept the answer. Analyses store a question and a calculated revision that cites the dataset version and metric version used. Those records stay in schema `app`.
