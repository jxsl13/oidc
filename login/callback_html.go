package login

var (
	// DefaultOkCallbackHTML will be shown when the browserlogin flow succeeds
	DefaultOkCallbackHTML = `
<html>
	<head>
	</head>
	<body >
	    Successfully authorized.</br>
	    This page will be closed in a few seconds.
	    <script>
		    setTimeout(function() {window.close()}, 5000);
	    </script>
	</body>
</html>
`
	// DefaultErrCallbackHTML is shown when the browser lofin flow fails.
	DefaultErrCallbackHTML = `
	<html>
		<head>
		</head>
		<body >
			Authorization failed.</br>
			This page will be closed in a few seconds.
			<script>
				setTimeout(function() {window.close()}, 5000);
			</script>
		</body>
	</html>
	`
)
