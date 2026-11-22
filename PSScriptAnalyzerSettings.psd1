# PSScriptAnalyzer settings for inat's PowerShell scripts.
#
# Rules excluded — with justification:
#
# PSAvoidUsingWriteHost
#   Installer UX wants coloured terminal output that reaches the user
#   even when the script runs inside `iwr ... | iex`. Write-Output
#   and Write-Information are the wrong channels here (they would be
#   consumed by the pipeline or suppressed by default). Write-Host is
#   the right primitive for top-of-funnel install messaging.
#
# PSUseShouldProcessForStateChangingFunctions
#   install.ps1 / uninstall.ps1 are user-invoked scripts, not module
#   cmdlets. Adding `-WhatIf` / `-Confirm` boilerplate to every verb
#   starting with Stop/Update/Remove does not improve safety for a
#   `curl | sh` -shaped install flow; the flags the user actually
#   holds are `-Version`, `-PreRelease`, `-NoModifyPath`, documented
#   in spec 0002 section 9.4.

@{
    ExcludeRules = @(
        'PSAvoidUsingWriteHost',
        'PSUseShouldProcessForStateChangingFunctions'
    )
}
