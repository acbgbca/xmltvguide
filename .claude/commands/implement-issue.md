---
name: implement-issue
description: Implements the provided GitHub issue
---

You have been provided a link to a GitHub issue. If no link was provided ask the user to provide a link to a GitHub issue.

Before you do anything, checkout the main branch and fetch all changs from origin. Then read the GitHub issue, and let me know if you have any questions? Once you are clear on what needs to be done:

* Create a new branch that references the issue
* We are using a TDD workflow, so create the tests first and implement just enough code for the tests to compile. Make sure the tests fail.
* Implement the change as outlined in the issue
* Verify that all the tests pass with the change. If the tests fail, assume your changes have caused the tests to fail. The tests should always pass on main.
* Commit the change, and then ask me to review
* If I provide feedback, update the changes accordingly, commit the change, and ask the user to review again
* Once I have approved the change, push the branch and create a pull request

Your work is now complete, do not ask to implement another issue.

The user may come back with a bug, if so follow the process above by reproducing the bug in a test, and then fixing the issue or making the requested change
