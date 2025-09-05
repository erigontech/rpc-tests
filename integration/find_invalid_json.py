
import os
import json
import tarfile

def find_invalid_json(directory):
    """
    Scansiona una directory per trovare file .json e .tar che contengono JSON
    con la chiave 'response' senza i campi 'id' o 'jsonrpc'.
    """
    invalid_files = []
    
    # Cammina attraverso la directory e le sottodirectory
    for root, _, files in os.walk(directory):
        for filename in files:
            filepath = os.path.join(root, filename)
            
            # Controlla i file .json
            if filename.endswith('.json'):
                try:
                    with open(filepath, 'r') as f:
                        data = json.load(f)
                        if isinstance(data, list):
                            for item in data:
                                if 'response' in item and ('id' not in item['response'] or 'jsonrpc' not in item['response']):
                                    invalid_files.append(filepath)
                                    break  # Passa al prossimo file una volta trovato un errore
                        elif isinstance(data, dict):
                            if 'response' in data and ('id' not in data['response'] or 'jsonrpc' not in data['response']):
                                invalid_files.append(filepath)
                except (json.JSONDecodeError, KeyError) as e:
                    print(f"Errore di lettura o decodifica JSON in {filepath}: {e}")
            
            # Controlla i file .tar
            elif filename.endswith('.tar'):
                try:
                    with tarfile.open(filepath, 'r') as tar:
                        for member in tar.getmembers():
                            if member.isfile() and member.name.endswith('.json'):
                                f = tar.extractfile(member)
                                if f:
                                    content = f.read()
                                    data = json.loads(content)
                                    if isinstance(data, list):
                                        for item in data:
                                            if 'response' in item and ('id' not in item['response'] or 'jsonrpc' not in item['response']):
                                                invalid_files.append(f"{filepath} (dentro {member.name})")
                                                break
                                    elif isinstance(data, dict):
                                        if 'response' in data and ('id' not in data['response'] or 'jsonrpc' not in data['response']):
                                            invalid_files.append(f"{filepath} (dentro {member.name})")
                except tarfile.TarError as e:
                    print(f"Errore nella lettura del file tar {filepath}: {e}")
                except (json.JSONDecodeError, KeyError) as e:
                    print(f"Errore di decodifica JSON in un file dentro {filepath}: {e}")
    
    return invalid_files

if __name__ == "__main__":
    import argparse
    
    parser = argparse.ArgumentParser(description="Trova file JSON non validi in una directory.")
    parser.add_argument("directory", help="La directory da scansionare.")
    
    args = parser.parse_args()
    
    if not os.path.isdir(args.directory):
        print(f"Errore: La directory '{args.directory}' non esiste.")
    else:
        print(f"Scansione in corso della directory '{args.directory}'...")
        found_files = find_invalid_json(args.directory)
        
        if found_files:
            print("\n🚨 File con 'response' senza 'id' o 'jsonrpc':")
            for f in found_files:
                print(f"- {f}")
        else:
            print("\n✅ Nessun file non valido trovato.")
