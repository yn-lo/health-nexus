function allergySelector(initialValue) {
    var self = {
        selectedAllergies: [],
        customText: '',

        toggleAllergy(name) {
            var idx = this.selectedAllergies.indexOf(name);
            if (idx >= 0) {
                this.selectedAllergies.splice(idx, 1);
            } else {
                this.selectedAllergies.push(name);
            }
            this.syncToTextarea();
        },

        isSelected(name) {
            return this.selectedAllergies.indexOf(name) >= 0;
        },

        syncToTextarea() {
            this.customText = this.selectedAllergies.join('，');
        },

        init(value) {
            if (value && value.trim()) {
                this.customText = value;
            }
        }
    };

    if (initialValue && initialValue.trim()) {
        self.customText = initialValue;
    }
    return self;
}
