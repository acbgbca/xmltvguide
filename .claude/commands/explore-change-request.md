---
name: explore-change-request
description: Explore a change request in a GitHub issue
---

You have been provided a link to a GitHub issue. If no link was provided ask the user to provide a link to a GitHub issue.

Read the GitHub issue and see if you understand the request. Ask additional clarifying questions until you are sure that you understand the need the user has.

If the problem is a simple change, propose a solution to the user. If they accept add that solution to the GitHub issue, add the 'Ready for Development' label to that issue, and then finish, your work is done.

If the problem is a complex change, explore the code base and research possible solutions to the problem. Work with the user across several phases:
* Present initial options, and ask the user which to investigate in greater detail
* Investigate the options identified by the user, and present back some high level plans
* The user will select which plan to proceed with

Once the user has selected the plan, come up with a complete plan to implement the change. Break it down into several phases, and present it back to the user for approval. Once the user has approved the change, create a GitHub issue for each phase, including sufficient information that an agent with no prior context could implement that change. Add the 'Ready for Development' label to each issue, and link them to this issue. The last issue should indicate that it closes this change request.

Once the plan is completed, do not offer to start the change, that will be done by another agent. Your work is complete.
