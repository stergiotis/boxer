# boxer [![Go Reference](https://pkg.go.dev/badge/github.com/stergiotis/boxer.svg)](https://pkg.go.dev/github.com/stergiotis/boxer) [![Go Report Card](https://goreportcard.com/badge/github.com/stergiotis/boxer)](https://goreportcard.com/report/github.com/stergiotis/boxer)
Go packages helping to win by K.O. when fighting cross-cutting concerns.

## Goals
* Apply **low allocation** coding practices;
* use **data oriented programming** whenever appropriate;
* introduce as little [**runtime dependencies**](https://deps.dev/go/github.com%252Fstergiotis%252Fboxer) as possible;
* have **predictable performance**;
* provide production grade **error reporting**.

## Installation
``
go get github.com/stergiotis/boxer
``

## Maturity
Unstable, alpha, API may still change heavily.

## Glossary
<dl>
<dt>curlier</dt><dd>Go code mimicking <a href="https://curl.se/">cUrl</a>.</dd>
<dt>e2e</dt><dd>End-to-end.</dd>
<dt>ea</dt><dd>Means input-output (german abbreviation to distinguish from core packages).</dd>
<dt>eb</dt><dd>Error building.</dd>
<dt>eh</dt><dd>Error handling.</dd>
<dt>fec</dt><dd>Forward error correction.</dd>
<dt>inst</dt><dd>Instance (similar to self, this).</dd>
</dl>

## Contributing
Currently, no third-party contributions are accepted.

## License
The MIT License (MIT) 2023 - [Panos Stergiotis](https://github.com/stergiotis/). See [LICENSE](LICENSE) for the full license text.