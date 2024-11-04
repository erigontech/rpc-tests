import tarfile
import json
import sys

if len(sys.argv) != 2:
    print("Usage: python script.py <tar_file>")
    sys.exit(1)

tar_file = sys.argv[1]

with tarfile.open(tar_file, encoding='utf-8') as tar:
    files = tar.getmembers()
    print(files)
    if len(files) != 1:
        print("bad archive file " + tar_file + ", expected 1 file, got " + str(len(files)))
        sys.exit(1)
    file = tar.extractfile(files[0])
    buff = file.read()
    tar.close()
    jsonrpc_commands = json.loads(buff)