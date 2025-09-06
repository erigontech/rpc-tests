
import os
import json
import tarfile

def find_invalid_json(directory):
    """
    Scansiona una directory per trovare file .json e .tar che contengono un oggetto 'response'
    con JSON-RPC non validi (mancano le chiavi 'id' o 'jsonrpc').
    """
    invalid_files = []

    def is_invalid_jsonrpc_response(item):
        """Valida un singolo oggetto di risposta JSON-RPC con una logica meno stringente."""
        # Un oggetto è invalido se non è un dizionario o se mancano le chiavi 'id' o 'jsonrpc'.
        if not isinstance(item, dict) or 'id' not in item or 'jsonrpc' not in item:
            return True
        # Rimuoviamo il controllo su 'result'/'error' per renderlo più permissivo.
        return False

    def process_json_data(data, source_path):
        """Processa i dati JSON e aggiunge il file alla lista se la risposta è invalida."""
        is_invalid = False
        
        if isinstance(data, list):
            for top_level_item in data:
                if not isinstance(top_level_item, dict):
                    is_invalid = True
                    break
                
                response_data = top_level_item.get('response')
                if response_data is None:
                    # Se non c'è una chiave 'response', l'oggetto stesso deve essere una risposta valida.
                    if is_invalid_jsonrpc_response(top_level_item):
                        is_invalid = True
                        break
                elif isinstance(response_data, list):
                    for item in response_data:
                        if is_invalid_jsonrpc_response(item):
                            is_invalid = True
                            break
                elif isinstance(response_data, dict):
                    if is_invalid_jsonrpc_response(response_data):
                        is_invalid = True
                else:
                    is_invalid = True
                
                if is_invalid:
                    break
        elif isinstance(data, dict):
            response_data = data.get('response')
            if response_data is None:
                # Se non c'è una chiave 'response', l'oggetto stesso deve essere una risposta valida.
                if is_invalid_jsonrpc_response(data):
                    is_invalid = True
            elif isinstance(response_data, list):
                for item in response_data:
                    if is_invalid_jsonrpc_response(item):
                        is_invalid = True
                        break
            elif isinstance(response_data, dict):
                if is_invalid_jsonrpc_response(response_data):
                    is_invalid = True
            else:
                is_invalid = True
        else:
            is_invalid = True
        
        if is_invalid:
            invalid_files.append(source_path)

    # Scansione della directory
    for root, _, files in os.walk(directory):
        for filename in files:
            filepath = os.path.join(root, filename)
            
            if filename.endswith('.json'):
                try:
                    with open(filepath, 'r', encoding='utf-8') as f:
                        data = json.load(f)
                        process_json_data(data, filepath)
                except (json.JSONDecodeError, KeyError) as e:
                    print(f"Errore durante la lettura di {filepath}: {e}")
                    invalid_files.append(filepath)
            elif filename.endswith('.tar'):
                try:
                    with tarfile.open(filepath, 'r') as tar:
                        for member in tar.getmembers():
                            if member.isfile() and member.name.endswith('.json'):
                                f = tar.extractfile(member)
                                if f:
                                    content = f.read()
                                    try:
                                        data = json.loads(content.decode('utf-8'))
                                        process_json_data(data, f"{filepath} (dentro {member.name})")
                                    except (json.JSONDecodeError, KeyError) as e:
                                        print(f"Errore durante la lettura di un file in {filepath}: {e}")
                                        invalid_files.append(f"{filepath} (dentro {member.name})")
                except tarfile.TarError as e:
                    print(f"Errore nella lettura del file tar {filepath}: {e}")
                    invalid_files.append(filepath)
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
