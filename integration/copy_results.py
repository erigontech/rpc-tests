#!/usr/bin/python3
""" Copy the response json from result dir into rpc-tests dir """


import os
import re
import json
import tarfile
import glob
import sys

# --- Funzioni di Supporto ---

def extract_number(filename):
    """ extract number from test namee """
    match = re.search(r'\d+', filename)
    if match:
        return int(match.group())
    return 0

def find_and_read_response_json(search_dir, test_base_name):
    """ 
    Cerca e legge il file JSON di risposta, restituendo l'intero oggetto o lista di risposte
    JSON-RPC.
    """
    if not os.path.isdir(search_dir):
        print(f"Errore: La directory '{search_dir}' non esiste.")
        return None

    search_pattern = os.path.join(search_dir, f"{test_base_name}*response*.json")
    found_files = glob.glob(search_pattern)

    if not found_files:
        print(f"Nessun file di risposta trovato per il pattern '{test_base_name}*response*.json' in {search_dir}")
        return None

    file_path = found_files[0]
    print(f"Trovato potenziale file di risposta: {file_path}")

    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            data = json.load(f)

        # Function to validate a single JSON-RPC response object
        def is_valid_jsonrpc_response(item):
            if not isinstance(item, dict):
                return False
            # Check for 'id' and 'jsonrpc' keys
            if 'id' not in item or 'jsonrpc' not in item:
                return False
            # Check for either 'result' or 'error' key
            if ('result' not in item and 'error' not in item) or \
               ('result' in item and 'error' in item):
                return False
            return True

        # Check for both a single object and a list of objects
        if isinstance(data, list):
            if data and all(is_valid_jsonrpc_response(item) for item in data):
                print(f"Trovata lista di risposte JSON-RPC valide in: {file_path}")
                return data
        elif is_valid_jsonrpc_response(data):
            print(f"Trovato oggetto di risposta JSON-RPC valido in: {file_path}")
            return data
        print(f"Il file '{file_path}' non è un oggetto di risposta JSON-RPC valido o una lista di tali oggetti.")
        return None
    except (json.JSONDecodeError, FileNotFoundError) as e:
        print(f"Errore durante la lettura del file di risposta {file_path}: {e}")
        return None

def update_response_in_json_data(data, new_response_data, test_base_name):
    """
    Aggiorna la risposta nel JSON, gestendo sia dizionari che liste.
    Non aggiorna se la risposta originale ha un campo 'result' nullo o non ha 'result'.
    Restituisce True se il contenuto è stato modificato, False altrimenti.
    """
    if not new_response_data:
        print(f"Dati di risposta vuoti per '{test_base_name}'. Nessun aggiornamento.")
        return False

    modified = False

    def should_not_update(current_response):
        """
        Determina se la risposta attuale NON deve essere aggiornata.

        Si aggiorna se:
        1. Contiene un campo 'result' non nullo (risposta di successo).
        2. Contiene un campo 'error' non nullo (risposta di fallimento/errore RPC).

        NON si aggiorna (ritorna True) se:
        1. Il formato non è un dizionario valido.
        2. Non ha né il campo 'result' né il campo 'error' (risposta RPC incompleta o vuota).
        3. Ha il campo 'result' ma è nullo e manca il campo 'error'.
        """
        # 1. Aggiorna se il formato non è un dizionario (perché è un errore/formato non valido)
        if not isinstance(current_response, dict):
            return False
        # Controlla l'esistenza e la non-nullità di 'result' o 'error'.
        has_valid_result = "result" in current_response and current_response.get("result") is not None
        has_valid_error = "error" in current_response and current_response.get("error") is not None
        # Se ha un risultato valido O un errore valido, ALLORA DEVE AGGIORNARE,
        # quindi la funzione should_not_update deve ritornare False.
        if has_valid_result or has_valid_error:
            return False
        # In tutti gli altri casi (risposta vuota, solo "result": null, mancanza di entrambi i campi),
        # NON si aggiorna (ritorna True).
        return True

    # CASO 1: Il JSON è un DIZIONARIO (con chiave 'response')
    if isinstance(data, dict) and 'response' in data:
        current_response = data['response']
        if not should_not_update(current_response):
            if current_response != new_response_data:
                data['response'] = new_response_data
                modified = True
                print(f"Aggiornata la risposta in '{test_base_name}' (formato dizionario).")
            else:
                print(f"Nessun aggiornamento necessario per '{test_base_name}'. I dati sono già aggiornati.")
        else:
            print(f"La risposta originale è vuota o ha result=null per '{test_base_name}'. Non verrà aggiornata.")

    # CASO 2: Il JSON è una LISTA (di oggetti con chiave 'response' o l'intera lista è di risposte)
    elif isinstance(data, list):
        for item in data:
            if isinstance(item, dict) and 'response' in item:
                current_response = item['response']
                if not should_not_update(current_response):
                    if current_response != new_response_data:
                        item['response'] = new_response_data
                        modified = True
                        print(f"Aggiornata la risposta in '{test_base_name}' (lista di risposte).")
                        # Aggiorna solo il primo elemento che trovi e poi esci.
                        break
                    print(f"Nessun aggiornamento necessario per '{test_base_name}'. I dati sono già aggiornati.")
                    break
                print(f"La risposta originale è vuota o ha result=null in un elemento di '{test_base_name}'. Non verrà aggiornata.")
                # Anche in questo caso, non fare più controlli
                break

        # Gestisci il caso in cui l'intera lista è una risposta batch di JSON-RPC
        if not modified and len(data) > 0 and 'result' not in data[0] and 'error' not in data[0]:
            # Qui si potrebbe aggiungere una logica per gestire i casi in cui l'intera
            # lista è la risposta, ma questo richiede una logica specifica che non era
            # presente nella tua richiesta originale.
            pass

    return modified

# --- process_single_test_json_for_response_sync (rimane invariata, usa update_response_in_json_data) ---
def process_single_test_json_for_response_sync(filepath, result_api_dir):
    """ process single test json file """
    print(f"Elaborazione file JSON di test: {filepath}")

    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            data = json.load(f)

        test_base_name = os.path.splitext(os.path.basename(filepath))[0]
        full_response_from_result = find_and_read_response_json(result_api_dir, test_base_name)
        was_modified = update_response_in_json_data(data, full_response_from_result, test_base_name)
        if was_modified:
            with open(filepath, 'w', encoding='utf-8') as f_out:
                json.dump(data, f_out, indent=2)
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
    """ process tar file """
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
    return False

def main():
    """ main """
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
