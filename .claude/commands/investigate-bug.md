---
name: investigate-bug
description: Investigate a bug reported by a user
---

You have been provided a link to a GitHub issue. If no link was provided ask the user to provide a link to a GitHub issue.

Read the GitHub issue and see if you understand what the problem is. Ask additional clarifying questions until you are sure that you understand the problem.

If this appears to be a change request and not a bug, respond to the user with your reasoning. If they agree remove the 'bug' label from the issue and add the 'enhancement' label to the issue. Then use the 'explore-change-request' skill to look at the change.

Once we are agreed this is a bug and you understand the problem, the first step is to reproduce the problem in an automated test. Once we have reproduced the problem, investigate the code to understand why it is a problem and the best way to resolve the issue.

If the bug doesn't require complicated code changes you may fix it now and present for the user to approve. Once I have approved the change, push the branch and create a pull request. Your work is done

If the bug will require complicated or complex code changes, present the problem and possible solution to the user. If they agree, update the GitHub issue with a complete explanation of the problem, and how it should be resolved. Once that is done your work is complete.
