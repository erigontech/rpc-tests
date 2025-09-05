
import os
import re
import json
import tarfile
import shutil
import glob
import sys

# --- Funzioni di Supporto ---

def extract_number(filename):
    match = re.search(r'\d+', filename)
    if match:
        return int(match.group())
    else:
        return 0

def find_and_read_response_json(search_dir, test_base_name):
    """
    Cerca e legge il file JSON di risposta, restituendo l'intero oggetto di risposta
    JSON-RPC che contiene i campi 'id', 'jsonrpc' e 'result'.
    """
    # Costruisci il pattern di ricerca per il file di risposta
    search_pattern = os.path.join(search_dir, f"{test_base_name}*response*.json")

    # Usa glob per trovare il file corrispondente al pattern
    found_files = glob.glob(search_pattern)

    if not found_files:
        print(f"  Nessun file di risposta contenente 'response' trovato per '{test_base_name}' in {search_dir}")
        return None

    # Prendi il primo file trovato
    file_path = found_files[0]

    try:
        with open(file_path, 'r') as f:
            data = json.load(f)
            # Verifica che il JSON sia un oggetto JSON-RPC valido e completo
            if isinstance(data, dict) and 'id' in data and 'jsonrpc' in data and 'result' in data:
                print(f"  Trovato file di risposta JSON-RPC: {file_path}")
                return data
    except (json.JSONDecodeError, FileNotFoundError) as e:
        print(f"  Errore durante la lettura del file di risposta {file_path}: {e}")
        return None
    
    # Se il file non è un oggetto JSON-RPC valido, ritorna None
    return None


def update_response_in_json_data(data, new_response_data, test_base_name):
    """
    Aggiorna la risposta nel JSON del tar, gestendo sia dizionari che liste.
    Restituisce True se il contenuto è stato modificato, False altrimenti.
    """
    if new_response_data is None:
        print(f"  Dati di risposta vuoti per '{test_base_name}'. Nessun aggiornamento.")
        return False
    
    modified = False

    # CASO 1: Il JSON interno è un DIZIONARIO (vecchio formato)
    if isinstance(data, dict):
        if 'response' in data and data['response'] != new_response_data:
            data['response'] = new_response_data
            modified = True

    # CASO 2: Il JSON interno è una LISTA (nuovo formato)
    elif isinstance(data, list):
        print(f"  [LISTA] Aggiornamento dell'elemento 0 in '{test_base_name}.json'.")
        # Ipotizziamo che tu voglia aggiornare solo il primo elemento della lista
        if len(data) > 0 and 'response' in data[0]:
            # Questo è il punto in cui il tuo codice deve agire
            # Assumendo che la risposta sia un campo all'interno del primo oggetto della lista
            data[0]['response'] = new_response_data
            modified = True
        else:
            print(f"  [LISTA] Errore: l'elemento 0 non ha un campo 'response' o la lista è vuota.")
            
    # Gestisci anche il caso in cui il JSON stesso è solo un array di risposte
    elif data == new_response_data:
        # Se i dati interni sono già uguali ai dati di risposta, non fare nulla
        pass
    else:
        # Se il JSON è una lista e la risposta è un oggetto, sostituisci
        # L'intero contenuto della lista con la nuova risposta.
        # Questa logica potrebbe essere specifica al tuo caso.
        # data[:] = new_response_data
        pass
        
    return modified

# --- process_single_test_json_for_response_sync (rimane invariata, usa update_response_in_json_data) ---
def process_single_test_json_for_response_sync(filepath, result_api_dir):
    print(f"Elaborazione file JSON di test: {filepath}")
    
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            data = json.load(f)
        
        test_base_name = os.path.splitext(os.path.basename(filepath))[0]

        full_response_from_result = find_and_read_response_json(result_api_dir, test_base_name)
        
        was_modified = update_response_in_json_data(data, full_response_from_result, test_base_name)

        if was_modified:
            with open(filepath, 'w', encoding='utf-8') as f_out:
                json.dump(data, f_out, indent=4)
                print(f"  **{os.path.basename(filepath)}** aggiornato su disco.")
        else:
            print(f"  Nessuna modifica rilevata per {os.path.basename(filepath)}. File non riscritto.")
        
        return was_modified

    except json.JSONDecodeError as e:
        print(f"Errore nella decodifica del file JSON {filepath}: {e}. Salto.")
        return False
    except Exception as e:
        print(f"Si è verificato un errore inatteso durante l'elaborazione di {filepath}: {e}")
        return False

def process_tar_file_for_response_sync(filepath, result_api_dir):
    print(f"Elaborazione file TAR: {filepath}")
    
    tar_dirname = os.path.dirname(filepath)
    tar_basename = os.path.basename(filepath)
    tar_name_without_ext = os.path.splitext(tar_basename)[0]
    
    temp_json_file_path_for_tar_add = os.path.join(tar_dirname, f"{tar_name_without_ext}_temp.json")
    
    try:
        with tarfile.open(filepath, 'r') as tar:
            internal_json_filename = None
            # Trova il nome del file JSON interno. Questo rende lo script più robusto.
            for member in tar.getmembers():
                if member.name.endswith('.json') and member.name.startswith(tar_name_without_ext):
                    internal_json_filename = member.name
                    break
            
            if not internal_json_filename:
                print(f"  Errore: Nessun file JSON corrispondente a '{tar_name_without_ext}' trovato nel TAR. Salto.")
                return False
                
            print(f"  Estrazione del JSON interno '{internal_json_filename}' dal TAR...")
            extracted_json_file_obj = tar.extractfile(internal_json_filename)
            if extracted_json_file_obj:
                json_content_str_original = extracted_json_file_obj.read().decode('utf-8')
                extracted_json_file_obj.close()
            else:
                print(f"  Impossibile leggere il contenuto di '{internal_json_filename}' da '{tar_basename}'. Salto.")
                return False
    
        data = json.loads(json_content_str_original)
        
        test_base_name = tar_name_without_ext
        
        full_response_from_result = find_and_read_response_json(result_api_dir, test_base_name)
        
        tar_content_modified = update_response_in_json_data(data, full_response_from_result, test_base_name)

        if tar_content_modified:
            print(f"  Ri-creazione dell'archivio TAR '{tar_basename}' con contenuto modificato...")
            with open(temp_json_file_path_for_tar_add, 'w', encoding='utf-8') as temp_f:
                json.dump(data, temp_f, indent=4)
                
            with tarfile.open(filepath, 'w:bz2') as new_tar:
                new_tar.add(temp_json_file_path_for_tar_add, arcname=internal_json_filename)
            print(f"  File TAR aggiornato con successo: {filepath}")
        else:
            print(f"  JSON interno non modificato, salto la ri-creazione del TAR per {filepath}.")

    except tarfile.ReadError:
        print(f"  Errore: Impossibile leggere il file TAR {filepath}. Potrebbe essere corrotto o non essere un archivio tar valido.")
    except Exception as e:
        print(f"  Si è verificato un errore inatteso durante l'elaborazione di {filepath}: {e}")
    finally:
        if os.path.exists(temp_json_file_path_for_tar_add):
            try:
                os.remove(temp_json_file_path_for_tar_add)
            except Exception as e:
                print(f"  Errore durante la pulizia del file temporaneo {os.path.basename(temp_json_file_path_for_tar_add)}: {e}")

def main():
    # 1. Gestione dell'argomento da riga di comando
    if len(sys.argv) < 2:
        print("Usage: python your_script_name.py <chain>")
        print("Supported chains: mainnet, gnosis")
        sys.exit(1)

    chain = sys.argv[1]
    
    if chain not in ['mainnet', 'gnosis']:
        print(f"Error: Unsupported chain '{chain}'. Supported chains are 'mainnet' and 'gnosis'.")
        sys.exit(1)

    # 2. Impostazione dinamica dei percorsi in base alla chain scelta
    base_dir = os.getcwd()
    chain_dir = os.path.join(base_dir, chain)
    result_dir = os.path.join(chain_dir, 'results')

    # Controlla se il percorso termina con 'integration'
    if not base_dir.endswith('integration'):
        print("La directory corrente termina con 'integration':", base_dir)
        return
    if not os.path.isdir(chain_dir):
        print(f"Error: The directory for chain '{chain}' specified '{chain_dir}' does not exist.")
        return
    if not os.path.isdir(result_dir):
        print(f"Error: The 'results' directory specified '{result_dir}' does not exist.")
        return

    api_subdirs_in_result = [d for d in os.listdir(result_dir) if os.path.isdir(os.path.join(result_dir, d))]

    if len(api_subdirs_in_result) != 1:
        print(f"Error: Expected exactly ONE API subdirectory in '{result_dir}', but found {len(api_subdirs_in_result)}.")
        print(f"Ensure '{result_dir}' contains only one test results directory at a time.")
        return

    api_name = api_subdirs_in_result[0]
    result_api_source_dir = os.path.join(result_dir, api_name)
    chain_target_api_dir = os.path.join(chain_dir, api_name)

    if not os.path.isdir(chain_target_api_dir):
        print(f"Warning: The corresponding API directory in '{chain}' ('{chain_target_api_dir}') does not exist. Skipping processing for this API.")
        return

    print(f"*** Starting 'response' section synchronization for API '{api_name}' on '{chain}' ***")
    print(f"    (Data taken from '{result_api_source_dir}' and applied to '{chain_target_api_dir}')")
    print("=" * 70)

    total_files_processed = 0

    for root, _, files in os.walk(chain_target_api_dir):
        sorted_files_in_dir = sorted(files, key=extract_number)
        for filename in sorted_files_in_dir:
            filepath = os.path.join(root, filename)

            if filename.endswith('.json'):
                print("-" * 50)
                process_single_test_json_for_response_sync(filepath, result_api_source_dir)
                total_files_processed += 1
            elif filename.endswith('.tar'):
                print("-" * 50)
                process_tar_file_for_response_sync(filepath, result_api_source_dir)
                total_files_processed += 1

    if total_files_processed == 0:
        print(f"No .json or .tar files found in '{chain_target_api_dir}' or its subdirectories.")
    else:
        print(f"\nSynchronization completed. Total files processed: {total_files_processed}.")
        print("=" * 70)

if __name__ == "__main__":
    main()
