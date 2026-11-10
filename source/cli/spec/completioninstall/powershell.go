package completioninstall

import (
	"fmt"
	"strings"

	"go.vipengele.org/commons/validate"
)

// PowerShellBinaryVar is the PowerShell variable the script embedding [PowerShellSnippet] sets to
// the path of the installed tool.
const PowerShellBinaryVar = "CompletionBinary"

// powershellTemplate is the PowerShell fragment [PowerShellSnippet] renders. Every variable it
// defines starts with ci, so it leaves the embedding script's own names alone. It is valid in
// Windows PowerShell 5.1: no ??, no ternary, no && and no $IsWindows, which 5.1 does not define.
const powershellTemplate = `# Install the shell completion of ` + snippetName + ` for PowerShell.
try {
	if ([System.IO.Path]::DirectorySeparatorChar -eq '\') {
		$ciHome = $env:USERPROFILE
		$ciBase = ''
		if ($ciHome) { $ciBase = [System.IO.Path]::Combine($ciHome, 'Documents', 'PowerShell') }
	} else {
		$ciBase = ''
		if ($env:XDG_CONFIG_HOME) {
			$ciBase = [System.IO.Path]::Combine($env:XDG_CONFIG_HOME, 'powershell')
		} elseif ($env:HOME) {
			$ciBase = [System.IO.Path]::Combine($env:HOME, '.config', 'powershell')
		}
	}
	if (-not $ciBase) { throw 'no home directory' }
	$ciDir = [System.IO.Path]::Combine($ciBase, 'Completions')
	$ciFile = [System.IO.Path]::Combine($ciDir, '` + snippetName + `.ps1')
	$ciOut = & $` + PowerShellBinaryVar + ` completion powershell
	if ($LASTEXITCODE -ne 0) { throw '` + snippetName + ` completion powershell failed' }
	New-Item -ItemType Directory -Path $ciDir -Force | Out-Null
	Set-Content -LiteralPath $ciFile -Value $ciOut -Encoding UTF8
	Write-Host ('Installed the PowerShell completion of ` + snippetName + ` at ' + $ciFile)
	Write-Host 'To load it, add this line to your $PROFILE:'
	Write-Host ('. ''' + $ciFile.Replace("'", "''") + '''')
} catch {
	Write-Warning ('skipping the shell completion: ' + $_.Exception.Message)
}
`

// PowerShellSnippet returns a PowerShell fragment for a tool's install script that installs the
// PowerShell completion of the tool name. name must be a slug, as for [Snippet]; any other name is
// an error wrapping [validate.ErrEmpty] or [validate.ErrInvalid].
//
// The embedding script sets the variable $CompletionBinary ([PowerShellBinaryVar]) to the path of
// the installed tool before the fragment runs. The fragment writes the output of
// `& $CompletionBinary completion powershell` to the file [Target] chooses for PowerShell,
// creating its directory: $env:USERPROFILE\Documents\PowerShell\Completions\<name>.ps1 on windows;
// elsewhere, under $env:XDG_CONFIG_HOME, or $env:HOME\.config when that is empty,
// powershell/Completions/<name>.ps1. It then prints the line `. '<file>'` for the user to add to
// $PROFILE and never edits the profile.
//
// The fragment carries no opt-out guard, as for [Snippet]: the embedding script skips it when its
// own <EnvPrefix>_NO_COMPLETION, named by the tool's [spec.Identity] and [NoCompletionEnvSuffix], is
// 1. Any failure, such as no home directory or a
// failing `completion powershell`, is a warning from Write-Warning: the fragment catches every
// terminating error, so it never ends the embedding script, whatever $ErrorActionPreference is.
// It runs on Windows PowerShell 5.1 and later.
func PowerShellSnippet(name string) (string, error) {
	if err := validate.Slug("name", name); err != nil {
		return "", fmt.Errorf("completioninstall: invalid tool name: %w", err)
	}
	return strings.ReplaceAll(powershellTemplate, snippetName, name), nil
}
