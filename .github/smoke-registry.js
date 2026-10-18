// Local registry for the yarn 1 smoke cell: serves the built @jayulabs/mugicrew-<platform>
// tarballs, so yarn 1's registry lookups of the optional platform packages succeed before publish.
// usage: node smoke-registry.js <dist> <version> <port>
const http = require('http'), fs = require('fs'), crypto = require('crypto'), { execFileSync } = require('child_process');
const [dist, version, port] = process.argv.slice(2);
http.createServer((req, res) => {
  res.setHeader('content-type', 'application/json');
  const path = decodeURIComponent(req.url.split('?')[0]).slice(1);
  const m = path.match(/^@jayulabs\/(mugicrew-[a-z0-9-]+)$/);
  const tgz = (n) => `${dist}/jayulabs-${n}-${version}.tgz`;
  if (m && fs.existsSync(tgz(m[1]))) {
    const manifest = JSON.parse(execFileSync('tar', ['-xzOf', tgz(m[1]), 'package/package.json']));
    const shasum = crypto.createHash('sha1').update(fs.readFileSync(tgz(m[1]))).digest('hex');
    manifest.dist = { tarball: `http://127.0.0.1:${port}/tarball/${m[1]}`, shasum };
    res.end(JSON.stringify({ name: manifest.name, 'dist-tags': { latest: version }, versions: { [version]: manifest } }));
  } else if (path.startsWith('tarball/') && fs.existsSync(tgz(path.slice(8)))) {
    res.end(fs.readFileSync(tgz(path.slice(8))));
  } else {
    res.statusCode = 404;
    res.end('{}');
  }
}).listen(port, '127.0.0.1');
